package center

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/diagnostic"
	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/packages/contracts"
)

func probeResultExpired(at any) bool {
	text, _ := at.(string)
	if text == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, text)
	}
	if err != nil {
		return true
	}
	return time.Since(parsed) > 14*24*time.Hour
}

type subscriptionNodeCheck struct {
	Index int `json:"index"`
	Name string `json:"name"`
	Check contracts.NodeProbeCheck `json:"check"`
}

func (s *Server) testSubscriptionProfile(c *gin.Context) {
	id,ok:=idParam(c);if !ok{return}
	var req struct {GateID int `json:"gate_id"`}
	if !bindWorker(c,&req,4096){return}
	if req.GateID<0 {failure(c,400,"invalid_gate_id");return}
	lockKey:="sub-probe:"+itoa(id)
	locked,err:=s.Store.KV.SetNX(c.Request.Context(),lockKey,"1",30*time.Minute)
	if err!=nil {failInternal(c,503,"probe_lock_failed",err);return}
	if !locked {failure(c,409,"probe_in_progress");return}
	defer s.Store.KV.Del(context.WithoutCancel(c.Request.Context()),lockKey)
	ctx,finish:=s.startSubscriptionRun(c.Request.Context(),id)
	var runErr error
	defer func(){finish(runErr)}()
	generation,_,err:=s.Store.KV.Get(ctx,"sub-group-generation")
	if err!=nil {runErr=err;failInternal(c,503,"cache_version_unavailable",err);return}
	p,err:=s.Store.Ent.SubProfile.Get(ctx,id)
	if err!=nil {runErr=err;failure(c,404,"profile_not_found");return}
	list,_,_,_,err:=s.groupNodes(ctx,p,"clash-meta/2.5.0",false)
	if err!=nil {runErr=err;failDetail(c,422,"subscription_probe_failed",err.Error(),nil);return}
	checks,err:=s.probeSubscriptionNodes(ctx,req.GateID,list)
	runErr=err
	location:="center";if req.GateID>0 {location="gate"}
	value:=map[string]any{"profile_id":id,"location":location,"execution_gate_id":req.GateID,"at":time.Now().UTC(),"trace_id":taskTrace(ctx),"checks":checks,"complete":runErr==nil,"generation":generation}
	cleanup,cancel:=context.WithTimeout(context.WithoutCancel(ctx),3*time.Second);defer cancel()
	if err=s.Store.Ent.Resource.Create().SetKind("subscription-probe").SetKey(itoa(id)).SetData(value).OnConflictColumns(resource.FieldKind,resource.FieldKey).UpdateNewValues().Exec(cleanup);err!=nil {runErr=err;failInternal(c,503,"save_failed",err);return}
	if runErr!=nil {failDetail(c,422,"subscription_probe_failed",safeCheckOutput(runErr.Error()),gin.H{"partial":value});return}
	s.audit(c,"subscription.profile.test","profile:"+itoa(id),"ok")
	c.JSON(200,value)
}

func (s *Server) getSubscriptionProfileTest(c *gin.Context) {
	id,ok:=idParam(c);if !ok{return}
	ctx:=c.Request.Context()
	if _,err:=s.Store.Ent.SubProfile.Get(ctx,id);err!=nil {failure(c,404,"profile_not_found");return}
	row,err:=s.Store.Ent.Resource.Query().Where(resource.KindEQ("subscription-probe"),resource.KeyEQ(itoa(id))).Only(ctx)
	if ent.IsNotFound(err) {c.JSON(200,gin.H{"result":nil});return}
	if err!=nil {failInternal(c,503,"query_failed",err);return}
	generation,_,err:=s.Store.KV.Get(ctx,"sub-group-generation")
	if err!=nil {failInternal(c,503,"cache_version_unavailable",err);return}
	if fmt.Sprint(row.Data["generation"]) != generation || probeResultExpired(row.Data["at"]) {c.JSON(200,gin.H{"result":nil});return}
	c.JSON(200,gin.H{"result":row.Data})
}

func (s *Server) probeSubscriptionNodes(ctx context.Context,gateID int,list string) (out []subscriptionNodeCheck,err error) {
	started:=time.Now().UTC()
	defer func(){subscriptionStageResult(ctx,0,"subscription.probe","center.probeSubscriptionNodes",started,err)}()
	lines:=strings.FieldsFunc(list,func(r rune)bool{return r=='\n'||r=='\r'})
	if len(lines)>maxSubscriptionMembers{return nil,errors.New("profile_expansion_exceeded")}
	out=make([]subscriptionNodeCheck,0,len(lines))
	ctx,cancel:=context.WithTimeout(ctx,30*time.Minute);defer cancel()
	for offset:=0;offset<len(lines);offset+=8 {
		if err=ctx.Err();err!=nil{return out,err}
		if job:=jobOf(ctx);job!=nil {job.Step(fmt.Sprintf("正在实测 %d/%d 个节点",offset,len(lines)))}
		end:=min(offset+8,len(lines))
		proxies:=[]map[string]any{}
		positions:=map[string]int{}
		for i:=offset;i<end;i++ {
			name:=fmt.Sprintf("probe-%d",i+1)
			proxy,e:=subout.ClashProxy(strings.TrimSpace(lines[i]))
			check:=subscriptionNodeCheck{Index:i+1,Name:name,Check:nodeprobe.Check{Status:"unknown"}}
			if e!=nil {check.Check.Why="当前客户端不支持此节点"} else {
				if original,ok:=proxy["name"].(string);ok {check.Name=diagnostic.Text(original)}
				proxy["name"]=name;proxies=append(proxies,proxy);positions[name]=len(out)
			}
			out=append(out,check)
		}
		if len(proxies)==0{continue}
		var results map[string]nodeprobe.Check
		if gateID>0 {
			g,e:=s.Store.Ent.Gate.Get(ctx,gateID);if e!=nil{return out,e}
			kernels,drivers,e:=s.gateKernels(ctx,g,map[string]bool{"mihomo":true});if e!=nil{return out,e}
			raw,_,e:=s.dispatchWorkerTask(ctx,contracts.TaskSubscriptionProbe,contracts.SubscriptionProbeTask{Proxies:proxies,Target:contracts.GroupProbeURL,Kernels:kernels,Drivers:drivers},contracts.TaskRequirements{TargetGateID:gateID},[]int{gateID});if e!=nil{return out,e}
			var result contracts.SubscriptionProbeResult
			if e=contracts.DecodeStrict(raw,&result,256<<10);e!=nil{return out,e}
			results=result.Checks
			for key,check:=range results {check.Why=probeReasonText(check.Why);results[key]=check}
		} else {
			bin,driver,e:=deployedProbeBinary(s,ctx);if e!=nil{return out,e}
			results,e=batchNodeProbe(ctx,nodeprobe.BatchOptions{Binary:bin,Run:driver.Run,Check:driver.Check,Proxies:proxies,Target:contracts.GroupProbeURL,PublicOnly:true});if e!=nil{return out,e}
		}
		for name,index:=range positions {
			check,ok:=results[name];if !ok{return out,errors.New("probe_result_members_invalid")}
			check.Why=safeCheckOutput(check.Why)
			out[index].Check=check
		}
		jobOutput(ctx,map[string]any{"checks":append([]subscriptionNodeCheck(nil),out...),"total":len(lines)})
	}
	return out,nil
}
