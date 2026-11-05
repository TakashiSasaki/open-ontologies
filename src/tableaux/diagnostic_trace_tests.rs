// SPDX-FileCopyrightText: 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
// SPDX-License-Identifier: MIT
// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
use super::*;

fn reasoner(turtle: &str) -> DlReasoner {
    let graph = Arc::new(GraphStore::new());
    graph.load_turtle(turtle, None).unwrap();
    DlReasoner::from_graph(&graph).unwrap()
}

#[test]
fn budget_exhaustion_is_unknown_not_satisfiable_or_not_subsumed() {
    let mut r = reasoner("<https://example.org/A> a <http://www.w3.org/2002/07/owl#Class> .");
    r.budget_ms = Some(0);
    let explain: serde_json::Value =
        serde_json::from_str(&r.explain_known_class("<https://example.org/A>").unwrap()).unwrap();
    assert!(explain["satisfiable"].is_null(), "{explain}");
    assert_eq!(explain["complete"], false);
    assert_eq!(explain["reason"], "budget_exhausted");
    let checked: serde_json::Value = serde_json::from_str(
        &r.check_known_subsumption("<https://example.org/A>", "<https://example.org/A>")
            .unwrap(),
    )
    .unwrap();
    assert!(checked["subsumed"].is_null(), "{checked}");
    assert_eq!(checked["complete"], false);
}

#[test]
fn failed_disjunction_branches_retain_their_clash_steps() {
    let r = reasoner(
        r#"
        @prefix ex: <https://example.org/> .
        @prefix owl: <http://www.w3.org/2002/07/owl#> .
        @prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
        ex:A a owl:Class ; owl:equivalentClass [owl:unionOf (ex:B ex:C)] .
        ex:B a owl:Class ; rdfs:subClassOf owl:Nothing .
        ex:C a owl:Class ; rdfs:subClassOf owl:Nothing .
    "#,
    );
    let result: serde_json::Value =
        serde_json::from_str(&r.explain_known_class("<https://example.org/A>").unwrap()).unwrap();
    assert_eq!(result["satisfiable"], false, "{result}");
    let steps = result["explanation"].as_array().unwrap();
    assert!(
        steps
            .iter()
            .filter(|s| s.as_str().unwrap().contains("Clash"))
            .count()
            >= 2,
        "{result}"
    );
}

#[test]
fn diagnostics_are_bounded_and_report_truncation() {
    let mut trace = ExplanationTrace::new(true);
    for _ in 0..1000 {
        trace.record("a recorded step");
    }
    assert_eq!(trace.steps.len(), 513);
    assert!(trace.steps.last().unwrap().contains("truncated"));
}

#[test]
fn public_class_apis_accept_bare_or_bracketed_iris() {
    let graph = Arc::new(GraphStore::new());
    graph
        .load_turtle("<urn:A> a <http://www.w3.org/2002/07/owl#Class> .", None)
        .unwrap();
    for iri in ["urn:A", "<urn:A>"] {
        let explained: serde_json::Value =
            serde_json::from_str(&DlReasoner::explain_class(&graph, iri).unwrap()).unwrap();
        assert_eq!(explained["satisfiable"], true);
        let checked: serde_json::Value =
            serde_json::from_str(&DlReasoner::check_subsumption(&graph, iri, iri).unwrap())
                .unwrap();
        assert_eq!(checked["subsumed"], true);
    }
    assert!(DlReasoner::explain_class(&graph, "not an IRI").is_err());
}
