# Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
# MIT. Synthetic disposable-data verification; never point at production.
import argparse, importlib.util, json, pathlib, subprocess, sys

p=argparse.ArgumentParser()
p.add_argument('--url', default='http://127.0.0.1:18082/mcp')
p.add_argument('--expected-version', required=True)
p.add_argument('--client-script', required=True)
p.add_argument('--planner', required=True)
p.add_argument('--out', type=pathlib.Path, required=True)
p.add_argument('--allow-disposable-data', action='store_true', required=True)
a=p.parse_args(); a.out.mkdir(parents=True, exist_ok=True)
spec=importlib.util.spec_from_file_location('client', a.client_script)
m=importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
c=m.Client(a.url, modern=False)
init=c.rpc('initialize', {'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'native-fork-qa','version':'1'}})
c.rpc('notifications/initialized',notification=True)
assert init['serverInfo']['version']==a.expected_version,init
checks=[]
def call(name,args=None,error=False):
    r=c.rpc('tools/call',{'name':name,'arguments':args or {}})
    body=json.loads(r['content'][0]['text'])
    assert bool(r.get('isError'))==error,(name,r)
    if not error:assert not (isinstance(body,dict) and body.get('error')),(name,body)
    return body

def compile_case(label, action, goal):
    result=call('onto_plan_compile_pddl',{'action_names':[action],'goal_ttl':goal})
    assert result['execution_ready'],result
    path=a.out/label;path.mkdir(exist_ok=True)
    (path/'compiled.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
    (path/'domain.pddl').write_text(result['domain']); (path/'problem.pddl').write_text(result['problem'])
    command=[sys.executable,str(pathlib.Path(a.planner).resolve()),'--overall-time-limit','30s','--plan-file',str((path/'sas_plan').resolve()),str((path/'domain.pddl').resolve()),str((path/'problem.pddl').resolve()),'--search','astar(blind())']
    r=subprocess.run(command,capture_output=True,text=True,cwd=path)
    (path/'planner.log').write_text(r.stdout+r.stderr)
    return result,path,r

try:
    status=call('onto_status');assert status['triples_loaded']==0,status
    assert call('onto_action_list')==[], 'QA requires empty action registry'
    assert call('onto_policy_list')==[], 'QA requires empty policy registry'
    tools=c.tools();checks.append('native_catalog_and_fork_identity')
    invalid=call('onto_validate',{'input':'broken turtle','inline':True});assert invalid['valid'] is False
    call('onto_policy_check',{'target_iris':['not an IRI']},error=True)
    checks.append('MCP_error_flag_and_valid_negative_findings')
    ontology='''@prefix owl: <http://www.w3.org/2002/07/owl#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
<urn:s> a <urn:C> .
<urn:Cat> a owl:Class; owl:disjointWith <urn:Dog> .
<urn:Dog> a owl:Class . <urn:CatDog> a owl:Class; rdfs:subClassOf <urn:Cat>, <urn:Dog> .
<urn:org> a <urn:Organization> .'''
    call('onto_load',{'turtle':ontology})
    count=call('onto_status')['triples_loaded']
    for iri in ['urn:CatDog','<urn:CatDog>']:
        e=call('onto_dl_explain',{'class_iri':iri})
        assert e['satisfiable'] is False and e['explanation'],e
        d=call('onto_dl_check',{'sub_class':iri,'super_class':'urn:Cat'})
        assert d['subsumed'] is True and d['trace'],d
    checks.append('native_bare_bracketed_IRI_and_nonempty_DL_diagnostics')
    action={'name':'assign','parameters':[{'name':'target'},{'name':'value'}], 'preconditions':['ASK { <{target}> a <urn:C> }'], 'effects':[{'kind':'add_triple','subject':'{target}','predicate':'urn:p','object':'{value}'}], 'reversible':False}
    call('onto_action_register',{'schema_json':json.dumps(action)})
    compiled,path,r=compile_case('happy','assign','<urn:s> <urn:p> "SN-003" .')
    assert r.returncode==0,(r.returncode,r.stdout,r.stderr)
    steps=[]
    for line in (path/'sas_plan').read_text().splitlines():
        if not line.startswith('('):continue
        op,*args=line.strip('()').split()
        params=compiled['parameter_symbols'][op]
        assert len(args)==len(params)
        steps.append({'action_name':compiled['action_symbols'][op],'bindings':{name:compiled['symbols'][value] for (_,name),value in zip(params,args)}})
    assert steps,steps
    report=call('onto_plan_validate',{'steps':steps,'goal_facts':[['<urn:s>','<urn:p>','"SN-003"']]})
    assert report['valid'] and report['unsatisfied_goals']==[],report
    assert call('onto_status')['triples_loaded']==count
    (path/'validated.json').write_text(json.dumps({'steps':steps,'validation':report},indent=2)+'\n')
    checks.append('generated_PDDL_Fast_Downward_plan_symbol_roundtrip_native_private_validation')
    actual=call('onto_action_apply',dict(steps[0],check_preconditions=False));assert actual['triples_added']==1,actual
    checks.append('native_apply_IRI_literal_and_actual_counts')
    # All aliases x=y are forced by the single urn:C subject. Native delete-wins
    # must make the old triple + progress goal unreachable, not a false plan.
    call('onto_load',{'turtle':'<urn:s> <urn:old> <urn:o> .'})
    alias={'name':'alias_delete','parameters':[{'name':'x'},{'name':'y'}], 'preconditions':['ASK { {x} a <urn:C> }','ASK { {y} a <urn:C> }'], 'effects':[{'kind':'add_triple','subject':'{x}','predicate':'urn:old','object':'urn:o'},{'kind':'remove_triple','subject':'{y}','predicate':'urn:old','object':'urn:o'},{'kind':'add_triple','subject':'{x}','predicate':'urn:progress','object':'urn:o'}], 'reversible':False}
    call('onto_action_register',{'schema_json':json.dumps(alias)})
    _,_,r=compile_case('alias-delete','alias_delete','<urn:s> <urn:old> <urn:o>; <urn:progress> <urn:o> .')
    assert r.returncode in (11,12),(r.returncode,r.stdout,r.stderr)
    checks.append('planner_preserves_native_delete_wins_for_aliased_parameters')
    call('onto_policy_register',{'name':'deny_org','effect':'deny','condition':'ASK { <{target}> a <urn:Organization> }'})
    for target in ['urn:org','<urn:org>']:
        assert call('onto_policy_check',{'target_iris':[target]})['verdict']=='deny'
    before=call('onto_status')['triples_loaded']
    call('onto_load',{'turtle':'<urn:org> <urn:new> 1 . <urn:free> <urn:new> 1 .'},error=True)
    call('onto_clear',error=True)
    assert call('onto_status')['triples_loaded']==before
    q=call('onto_query',{'query':'ASK { <urn:free> <urn:new> 1 }'});assert q['result'] is False,q
    restricted=call('onto_plan_compile_pddl',{'action_names':['assign']});assert restricted['execution_ready'] is False
    checks.append('native_guard_cannot_be_bypassed_by_load_or_clear_and_planner_reports_scope')
    call('onto_policy_register',{'name':'broken','effect':'deny','condition':'ASK { syntax broken }'})
    call('onto_policy_check',{'target_iris':['urn:free']},error=True)
    call('onto_load',{'turtle':'<urn:free> <urn:new> 1 .'},error=True)
    assert call('onto_status')['triples_loaded']==before
    checks.append('native_policy_query_errors_fail_closed_without_partial_writes')
    report={'serverInfo':init['serverInfo'],'tools':len(tools),'checks':checks,'final_triples':before}
    (a.out/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False))
finally:c.close()
