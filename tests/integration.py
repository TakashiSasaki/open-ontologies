#!/usr/bin/env python3
# Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
"""Exercise unmodified upstream behavior through the complete adapter chain.

Default tests never load/clear the shared graph. --allow-qa-writes is for a
dedicated disposable upstream container only, and adds write/file/provenance cases.
"""
import argparse
import concurrent.futures
import hashlib
import importlib.util
import json
from pathlib import Path
import datetime
import uuid

ONTOLOGY = '''@prefix ex: <https://example.org/adapter-test#> .
@prefix owl: <http://www.w3.org/2002/07/owl#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
ex:Cat a owl:Class ; owl:disjointWith ex:Dog . ex:Dog a owl:Class .
ex:CatDog a owl:Class ; rdfs:subClassOf ex:Cat, ex:Dog .'''
SHAPES = '''@prefix ex: <https://example.org/adapter-test#> .
@prefix sh: <http://www.w3.org/ns/shacl#> .
ex:Shape a sh:NodeShape ; sh:targetNode ex:CatDog ;
sh:property [ sh:path <http://www.w3.org/2000/01/rdf-schema#label> ; sh:minCount 1 ] .'''

def run(args):
    spec = importlib.util.spec_from_file_location('client', args.client_script)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    def connect():
        c = module.Client(args.url, modern=not args.legacy)
        if args.legacy:
            c.rpc('initialize', {'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'adapter-verification','version':'1'}})
            c.rpc('notifications/initialized',notification=True)
        else:
            c.rpc('server/discover')
        return c
    checks = []
    def call(c,name,arguments,error=False):
        r=c.rpc('tools/call',{'name':name,'arguments':arguments})
        value=json.loads(r['content'][0]['text'])
        failed=bool(r.get('isError') or value.get('error'))
        assert failed==error,(name,value)
        if not args.legacy:
            assert r['structuredContent']==value,(name,'structured JSON projection failed')
        return value
    c=connect()
    try:
        before=call(c,'onto_status',{})
        assert before['version']==args.expected_version,before['version']
        tools=c.tools();assert len(tools)==124
        annotated=sum(bool(t.get('annotations',{}).get('readOnlyHint')) for t in tools)
        if not args.legacy:assert annotated==38,annotated
        assert 'inline_data' in next(t['inputSchema']['properties'] for t in tools if t['name']=='onto_ingest')
        checks.append('catalog_and_annotations')
        for fmt,data in [('json','[{"id":1,"name":"Ada"}]'),('csv','id,name\n1,Ada\n'),('ndjson','{"id":1,"name":"Ada"}\n'),('xml','<root><row><id>1</id><name>Ada</name></row></root>'),('yaml','- id: 1\n  name: Ada\n')]:
            p=call(c,'onto_ingest',{'inline_data':data,'format':fmt,'dry_run':True})
            assert p['triples_loaded']==0 and p['triples_generated']>0
            checks.append('inline_'+fmt)
        for iri in ['https://example.org/adapter-test#CatDog','<https://example.org/adapter-test#CatDog>']:
            p=call(c,'onto_dl_explain',{'ontology_turtle':ONTOLOGY,'class_iri':iri})
            assert p['satisfiable'] is False and p['execution_scope']=='request' and p['persistent_store_modified'] is False
        checks.append('bare_and_bracketed_IRI')
        analysis=[('onto_reason',{'profile':'rdfs','materialize':True}),('onto_defects',{}),('onto_dl_check',{'sub_class':'https://example.org/adapter-test#CatDog','super_class':'https://example.org/adapter-test#Cat'}),('onto_justify',{'triple':'<https://example.org/adapter-test#CatDog> <http://www.w3.org/2000/01/rdf-schema#subClassOf> <https://example.org/adapter-test#Cat>'}),('onto_shacl',{'shapes':SHAPES,'inline':True})]
        for name,params in analysis:
            p=call(c,name,dict(params,ontology_turtle=ONTOLOGY))
            assert p['execution_scope']=='request' and p['persistent_store_modified'] is False,(name,p)
            checks.append('private_'+name)
        old='<https://example.org/A> a <https://example.org/C> .'
        new=old+'\n<https://example.org/A> <https://example.org/p> 1 .'
        p=call(c,'onto_diff',{'old_content':old,'new_content':new});assert p['added']==1 and p['removed']==0
        p=call(c,'onto_diff',{'old_path':None,'new_path':None,'old_content':old,'new_content':new});assert p['added']==1 and p['removed']==0
        p=call(c,'onto_diff',{'old_content':'[] a <urn:C> .','new_content':'[] a <urn:C> .'});assert p['added']==0 and p['removed']==0
        checks.append('inline_diff_and_blank_nodes')
        errors=[('onto_ingest',{'inline_data':'[]'}),('onto_ingest',{'inline_data':'[broken','format':'json','dry_run':True}),('onto_ingest',{'path':'/data/data.json','inline_data':'[]','format':'json'}),('onto_ingest',{'inline_data':'binary','format':'xlsx'}),('onto_diff',{'old_content':'<#relative> a <urn:C> .','new_content':old}),('onto_diff',{'old_path':'/data/a.ttl','old_content':old,'new_content':new}),('onto_reason',{'ontology_turtle':ONTOLOGY,'certificate_dir':'/data/not-created'}),('onto_justify',{'ontology_turtle':ONTOLOGY,'certificate_dir':'/data/not-created'}),('onto_defects',{'ontology_turtle':'x'*(1024*1024+1)})]
        for name,params in errors:call(c,name,params,error=True)
        checks.append('invalid_ambiguous_oversized_and_file_output_rejected')
        def private_call(i):
            client=connect()
            try:
                p=call(client,'onto_defects',{'ontology_turtle':ONTOLOGY if i%2 else '<urn:isolated> <urn:value> 7 .'})
                assert p['persistent_store_modified'] is False
            finally:client.close()
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:list(pool.map(private_call,range(16)))
        checks.append('16_concurrent_private_calls')
        after=call(c,'onto_status',{});assert before['triples_loaded']==after['triples_loaded']
        if args.allow_qa_writes:
            data='[{"id":1,"name":"Ada"}]'
            namespace='https://example.org/adapter-written/'+uuid.uuid4().hex+'/'
            p=call(c,'onto_ingest',{'inline_data':data,'format':'json','base_iri':namespace,'provenance':True})
            assert p['triples_loaded']>0 and p['provenance_triples']>0
            source=namespace+'source/inline-'+hashlib.sha256(data.encode()).hexdigest()
            q=call(c,'onto_query',{'query':'SELECT ?label WHERE { <'+source+'> <http://www.w3.org/2000/01/rdf-schema#label> ?label }'})
            assert q['results'] and 'request-' not in json.dumps(q)
            p=call(c,'onto_ingest',{'path':'/data/data.json','dry_run':True});assert p['triples_loaded']==0
            p=call(c,'onto_ingest',{'path':'/data/data.json','base_iri':namespace+'file/'});assert p['triples_loaded']>0
            p=call(c,'onto_diff',{'old_path':'/data/a.ttl','new_path':'/data/b.ttl'});assert p['added']==1
            for params in [{'old_path':'/data/a.ttl','new_content':'<https://example.org/A> a <https://example.org/Class> ; <https://example.org/p> 1 .'}, {'old_content':'<https://example.org/A> a <https://example.org/Class> .','new_path':'/data/b.ttl'}]:
                p=call(c,'onto_diff',params);assert p['added']==1 and p['removed']==0
            checks.append('explicit_QA_shared_writes_provenance_file_and_mixed_inputs')
        final=call(c,'onto_status',{})
        report={'checked_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'url':args.url,'native_version':before['version'],'tool_count':len(tools),'read_only':annotated,'private_triples_before':before['triples_loaded'],'private_triples_after':after['triples_loaded'],'final_triples':final['triples_loaded'],'allow_qa_writes':args.allow_qa_writes,'checks':checks,'engine_limitations':'Upstream DL traces/budget semantics are retained, not synthesized.'}
        args.output.write_text(json.dumps(report,indent=2)+'\n')
        print(json.dumps(report))
    finally:c.close()

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('url');p.add_argument('--legacy',action='store_true');p.add_argument('--allow-qa-writes',action='store_true');p.add_argument('--expected-version',default='2.1.0');p.add_argument('--client-script',type=Path,default=Path('/data/mcp-compat-proxy/main/scripts/check-http.py'));p.add_argument('--output',required=True,type=Path);run(p.parse_args())
