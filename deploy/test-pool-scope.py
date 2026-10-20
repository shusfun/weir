"""Local ownership/failed-recovery checks; never connects to a VPS."""
import copy
import unittest
from unittest.mock import patch
import production_pool_scope as scope
from pool_fixture import REMOTE,SERVER


class PoolRecovery(unittest.TestCase):
    def setUp(self):
        self.record={'fixture_prefix':'run-','cleanup':{'handler':'outbound-pool','device_id':1,
            'test_grants':[11,12],'pools':[],'task_ids':[],'services':{},
            'baseline':{'sources':[],'device-outbound':[],'device-routing':[]}}}
        self.pool={'id':20,'name':'run-pool','source_id':30,'version':1,'status':'applied'}
        self.source={'id':30,'data':{'name':'run-source'}}
        self.member={'id':31,'owner_id':1,'data':{'id':200001,'source':'subscription','subscription_id':20}}
        self.route={'id':32,'owner_id':1,'data':{'id':100001,'outbound_id':200001,'client_id':11}}
        self.rows={'sources':[self.source],'device-outbound':[self.member],'device-routing':[self.route]}

    def test_no_existing_pool_cross_device_or_other_user_write(self):
        with patch.object(scope,'pools',return_value=[self.pool]),patch.object(scope,'resources',side_effect=lambda _,k:self.rows[k]):
            scope.authorize(None,self.record,'POST','/api/v1/admin/devices/1/pools/20/preview',{})
            for path,data in [('/api/v1/admin/devices/2/pools/20/apply',{}),
                ('/api/v1/admin/devices/1/pools/21/apply',{}),
                ('/api/v1/admin/devices/1/tasks',{'operation':'routing.upsert','payload':{'outbound_id':200001,'client_id':99}}),
                ('/api/v1/admin/devices/1/tasks',{'operation':'core.stop','payload':{'core':'xray'}})]:
                with self.assertRaises(RuntimeError):scope.authorize(None,self.record,'POST',path,data)
            self.record['cleanup']['pools']=[self.pool]
            with self.assertRaises(RuntimeError):scope.authorize(None,self.record,'POST','/api/v1/admin/devices/1/pools/20/apply',{})

    def test_cleanup_orders_routes_pools_sources_and_verifies_baseline(self):
        current_pools=[self.pool];calls=[]
        outer=self
        class API:
            def request(self,method,path,data=None):
                calls.append(path)
                if path.endswith('/tasks'):outer.rows['device-routing']=[]
                elif path.endswith('/apply'):current_pools.clear();outer.rows['device-outbound']=[]
                elif method=='DELETE':outer.rows['sources']=[]
                return {'id':5}
            def wait_task(self,_):pass
        with patch.object(scope,'service_baseline',return_value={}),patch.object(scope,'tasks',return_value=[]),patch.object(scope,'pools',side_effect=lambda *_:list(current_pools)),patch.object(scope,'resources',side_effect=lambda _,k:list(self.rows[k])),patch('pool_fixture.cleanup_fixture') as fixture:
            scope.cleanup(API(),self.record)
            self.assertEqual(calls,['/api/v1/admin/devices/1/tasks','/api/v1/admin/devices/1/pools/20/apply','/api/v1/admin/resources/sources/30'])
            fixture.assert_called_once_with(self.record)
            scope.cleanup(API(),self.record)

    def test_unowned_new_route_blocks_cleanup(self):
        self.rows['device-routing'][0]['data']['client_id']=99
        with patch.object(scope,'tasks',return_value=[]),patch.object(scope,'pools',return_value=[self.pool]),patch.object(scope,'resources',side_effect=lambda _,k:self.rows[k]):
            with self.assertRaisesRegex(RuntimeError,'Unowned routing'):scope.cleanup(None,self.record)

    def test_full_orders_only_when_original_collection_is_empty(self):
        manual={'id':40,'owner_id':1,'data':{'id':401,'tag':'run-manual','source':'manual'}}
        self.rows['device-outbound'].append(manual)
        with patch.object(scope,'pools',return_value=[self.pool]),patch.object(scope,'resources',side_effect=lambda _,k:self.rows[k]):
            scope.authorize(None,self.record,'POST','/api/v1/admin/devices/1/tasks',{'operation':'outbound.reorder','payload':{'items':[{'id':401,'sort':0}]}})
            scope.authorize(None,self.record,'POST','/api/v1/admin/devices/1/pools/order',{'items':[{'id':20,'version':1}]})
            scope.authorize(None,self.record,'POST','/api/v1/admin/devices/1/tasks',{'operation':'outbound.probe','payload':{'ids':[401,200001]}})
            for op,payload in [('outbound.probe',{'ids':[999]}),('outbound.upsert',{'id':999,'tag':'run-new'}),('outbound.probe',{'ids':[401],'address':'internal'})]:
                with self.assertRaises(RuntimeError):scope.authorize(None,self.record,'POST','/api/v1/admin/devices/1/tasks',{'operation':op,'payload':payload})
            self.record['cleanup']['baseline']['device-outbound']=[manual]
            with self.assertRaises(RuntimeError):scope.authorize(None,self.record,'POST','/api/v1/admin/devices/1/tasks',{'operation':'outbound.reorder','payload':{'items':[{'id':401,'sort':0}]}})
            self.record['cleanup']['pools']=[self.pool]
            with self.assertRaises(RuntimeError):scope.authorize(None,self.record,'POST','/api/v1/admin/devices/1/pools/refresh',{})

    def test_catalog_import_requires_persisted_candidate_and_cleans_only_registered_tag(self):
        candidate={'protocol':'http','key':'a'*64,'tag':'public-http-'+'a'*16}
        self.record['cleanup']['catalog_candidates']=[candidate]
        self.rows['device-outbound'].append({'id':40,'owner_id':1,'data':{'id':401,'tag':candidate['tag'],'source':'manual'}})
        with patch.object(scope,'pools',return_value=[self.pool]),patch.object(scope,'resources',side_effect=lambda _,k:self.rows[k]):
            scope.authorize(None,self.record,'POST','/api/v1/admin/devices/1/proxy-catalog/import',{'protocol':'http','key':'a'*64})
            with self.assertRaises(RuntimeError):scope.authorize(None,self.record,'POST','/api/v1/admin/devices/1/proxy-catalog/import',{'protocol':'http','key':'b'*64})
            self.assertEqual([x['id'] for x in scope.owned_manual(None,self.record)],[40])
            self.rows['device-outbound'][-1]['data']['tag']='public-http-'+'b'*16
            with self.assertRaises(RuntimeError):scope.owned_manual(None,self.record)

    def test_source_observation_can_advance_but_unknown_configuration_cannot(self):
        old={'data':{'url':'https://example.com','filter_include':'x','last_update':'old','links':['old']}}
        new=copy.deepcopy(old);new['data'].update(last_update='new',links=['new'])
        self.assertEqual(scope.source_configuration(old),scope.source_configuration(new))
        new['data']['filter_include']='changed'
        self.assertNotEqual(scope.source_configuration(old),scope.source_configuration(new))
        # Both remote programs are fixed valid Python, no shell evaluation of input.
        compile(REMOTE,'owned-fixture','exec');compile(SERVER,'source-fixture','exec')


if __name__=='__main__':unittest.main()
