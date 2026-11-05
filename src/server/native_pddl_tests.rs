// SPDX-FileCopyrightText: 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
// SPDX-License-Identifier: MIT
// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
use crate::{
    dynamics::{self, ActionSchema, EffectSpec},
    graph::GraphStore,
    inputs::OntoPlanCompilePddlInput,
    policy::{self, PolicyRule},
    server::OpenOntologiesServer,
    state::StateDb,
};
use rmcp::handler::server::wrapper::Parameters;
use serde_json::Value;
use std::{path::Path, sync::Arc};

fn action() -> ActionSchema {
    ActionSchema {
        name: "create".into(),
        parameters: vec![],
        preconditions: vec![],
        effects: vec![EffectSpec::AddClass {
            iri: "urn:C".into(),
        }],
        reversible: false,
        description: None,
        outcomes: vec![],
    }
}
async fn compile(server: &OpenOntologiesServer, names: &[&str], goal: Option<&str>) -> Value {
    serde_json::from_str(
        &server
            .onto_plan_compile_pddl(Parameters(OntoPlanCompilePddlInput {
                domain_name: None,
                action_names: names.iter().map(|n| n.to_string()).collect(),
                goal_ttl: goal.map(str::to_string),
            }))
            .await,
    )
    .unwrap()
}

#[tokio::test]
async fn malformed_goal_or_missing_duplicate_action_is_an_error() {
    let db = StateDb::open(Path::new(":memory:")).unwrap();
    dynamics::register(&db, &action()).unwrap();
    let server = OpenOntologiesServer::new_with_graph(db, Arc::new(GraphStore::new()));
    for (names, goal) in [
        (vec!["missing"], None),
        (vec!["create", "create"], None),
        (vec!["create"], Some("invalid turtle")),
    ] {
        assert!(compile(&server, &names, goal).await["error"].is_string());
    }
}

#[tokio::test]
async fn native_guards_cannot_be_misrepresented_as_compiled_pddl() {
    let db = StateDb::open(Path::new(":memory:")).unwrap();
    dynamics::register(&db, &action()).unwrap();
    policy::register_rule(
        &db,
        &PolicyRule {
            name: "gate".into(),
            effect: "deny".into(),
            condition: "ASK { FILTER(false) }".into(),
            description: None,
        },
    )
    .unwrap();
    let server = OpenOntologiesServer::new_with_graph(db, Arc::new(GraphStore::new()));
    let result = compile(&server, &["create"], None).await;
    assert_eq!(result["translation_complete"], true);
    assert_eq!(result["execution_ready"], false);
    assert!(!result["translation_notes"].as_array().unwrap().is_empty());
}

#[tokio::test]
async fn planning_snapshot_is_never_silently_truncated() {
    let db = StateDb::open(Path::new(":memory:")).unwrap();
    let graph = Arc::new(GraphStore::new());
    let data = (0..10001)
        .map(|i| format!("<urn:s{i}> <urn:p> <urn:o> .\n"))
        .collect::<String>();
    graph.load_turtle(&data, None).unwrap();
    let server = OpenOntologiesServer::new_with_graph(db, graph);
    let result = compile(&server, &[], None).await;
    assert!(
        result["error"]
            .as_str()
            .unwrap()
            .contains("no silent truncation")
    );
}
