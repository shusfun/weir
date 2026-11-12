package contracts

import (
	"strings"
	"testing"
	"time"
)

func TestWorkerComponentsAreBoundedAndTyped(t *testing.T) {
	caps := CapabilityDocument{Protocol: AgentProtocol, Version: "test", Arch: "amd64", OS: "linux", Tasks: map[string]int{}, Components: map[string]ComponentCapability{"sing-box": {Version: "1.12"}}}
	if err := caps.Validate(); err != nil {
		t.Fatal(err)
	}
	caps.Components["laya"] = ComponentCapability{Models: map[string]string{"model": strings.Repeat("x", 201)}}
	if caps.Validate() == nil {
		t.Fatal("unbounded model metadata accepted")
	}
	resources := WorkerResources{Concurrency: 1, Components: map[string]ComponentObservation{"laya": {State: "available", ObservedAt: time.Now()}}}
	if err := resources.Validate(); err != nil {
		t.Fatal(err)
	}
	resources.Components["laya"] = ComponentObservation{State: "healthy", ObservedAt: time.Now()}
	if resources.Validate() == nil {
		t.Fatal("unregistered component state accepted")
	}
	resources.Components = map[string]ComponentObservation{"shell": {State: "running", ObservedAt: time.Now()}}
	if resources.Validate() == nil {
		t.Fatal("unregistered component accepted")
	}
}
