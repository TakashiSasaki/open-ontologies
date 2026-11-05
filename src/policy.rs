// SPDX-FileCopyrightText: 2026 Fabio Rovai
// SPDX-License-Identifier: MIT

//! ARGOS-style policy governance for state-changing actions (#40, ISWC 2025
//! WOP).
//!
//! Lightweight policy gate that sits between an `onto_certify_action` /
//! `onto_action_apply` call and execution. Policies are SPARQL ASK queries
//! parameterised by the proposed action's target IRIs; each policy returns
//! "allow" or "deny" with a human-readable rationale.
//!
//! ## Why this isn't `onto_certify_action`
//!
//! CIVeX gates on *causal* properties (utility, blast radius, identifiability).
//! ARGOS gates on *policy* properties (who is allowed to touch what, under
//! which clearance, during which window). The two layers compose: CIVeX
//! handles statistical risk, ARGOS handles authorisation.

use crate::graph::GraphStore;
use crate::rdf_bindings::{self, Bindings};
use crate::state::StateDb;
use oxigraph::model::Term;
use oxigraph::sparql::{PreparedSparqlQuery, QueryResults};
use serde::{Deserialize, Serialize};
use std::sync::Arc;

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct PolicyRule {
    pub name: String,
    /// One of `"allow"` or `"deny"`. The rule's verdict when the SPARQL
    /// ASK returns `true`.
    pub effect: String,
    /// SPARQL ASK. Can use the placeholder `{target}` which is substituted
    /// by each target IRI when the rule is checked.
    pub condition: String,
    #[serde(default)]
    pub description: Option<String>,
}

#[derive(Clone, Debug, Serialize)]
pub struct PolicyReport {
    /// Deny overrides; allow only when every rule evaluated successfully and
    /// no deny rule fired. Errors are returned as Err, never as allow.
    pub verdict: String,
    pub rule_results: Vec<RuleResult>,
}

#[derive(Clone, Debug, Serialize)]
pub struct RuleResult {
    pub rule_name: String,
    pub target: String,
    pub effect: String,
    pub fired: bool,
}

const ENSURE: &str = "
CREATE TABLE IF NOT EXISTS policy_rules (
    name TEXT PRIMARY KEY,
    effect TEXT NOT NULL CHECK (effect IN ('allow', 'deny')),
    condition TEXT NOT NULL,
    description TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
)";

fn ensure(db: &StateDb) -> anyhow::Result<()> {
    db.conn().execute(ENSURE, [])?;
    Ok(())
}

pub fn register_rule(db: &StateDb, rule: &PolicyRule) -> anyhow::Result<()> {
    if !matches!(rule.effect.as_str(), "allow" | "deny") {
        anyhow::bail!("invalid effect `{}`: must be allow or deny", rule.effect);
    }
    ensure(db)?;
    db.conn().execute(
        "INSERT OR REPLACE INTO policy_rules (name, effect, condition, description) VALUES (?1, ?2, ?3, ?4)",
        rusqlite::params![rule.name, rule.effect, rule.condition, rule.description],
    )?;
    Ok(())
}

/// Read a policy snapshot without creating a table or hiding corrupt rows.
pub(crate) fn read_rules(conn: &rusqlite::Connection) -> anyhow::Result<Vec<PolicyRule>> {
    let exists: bool = conn.query_row(
        "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='policy_rules')",
        [],
        |r| r.get(0),
    )?;
    if !exists {
        return Ok(Vec::new());
    }
    let mut stmt =
        conn.prepare("SELECT name,effect,condition,description FROM policy_rules ORDER BY name")?;
    Ok(stmt
        .query_map([], |r| {
            Ok(PolicyRule {
                name: r.get(0)?,
                effect: r.get(1)?,
                condition: r.get(2)?,
                description: r.get(3)?,
            })
        })?
        .collect::<Result<Vec<_>, _>>()?)
}

pub fn list_rules(db: &StateDb) -> anyhow::Result<Vec<PolicyRule>> {
    read_rules(&db.conn())
}

/// Evaluate rules with a caller-selected consistent RDF view. Bindings are
/// parsed RDF terms and query failures abort the complete decision.
pub(crate) fn evaluate(
    rules: &[PolicyRule],
    targets: &[Term],
    mut ask: impl FnMut(PreparedSparqlQuery) -> anyhow::Result<bool>,
) -> anyhow::Result<PolicyReport> {
    let mut results = Vec::new();
    let mut denied = false;
    for rule in rules {
        anyhow::ensure!(
            matches!(rule.effect.as_str(), "allow" | "deny"),
            "invalid policy effect for {}",
            rule.name
        );
        for target in targets {
            let values = Bindings::from([("target".into(), target.clone())]);
            let condition = match rdf_bindings::query_head(&rule.condition) {
                Ok(h) if h.text.eq_ignore_ascii_case("ASK") => rule.condition.clone(),
                Ok(h)
                    if ["SELECT", "CONSTRUCT", "DESCRIBE"]
                        .iter()
                        .any(|q| h.text.eq_ignore_ascii_case(q)) =>
                {
                    anyhow::bail!("policy condition must be ASK")
                }
                _ => format!("ASK {{ {} }}", rule.condition),
            };
            rdf_bindings::local_condition(&condition)?;
            let prepared = if matches!(target, Term::BlankNode(_)) {
                // SPARQL blank-node syntax denotes an existential variable,
                // not a stored node identity. Project a substituted variable.
                let text = rdf_bindings::render(&condition, &values, true)?;
                if text.contains("?__oo_bound_0") {
                    let head = rdf_bindings::query_head(&text)?;
                    let text = format!(
                        "{} SELECT ?__oo_bound_0 {}",
                        &text[..head.start],
                        &text[head.end..]
                    );
                    oxigraph::sparql::SparqlEvaluator::new()
                        .parse_query(&text)?
                        .substitute_variable(
                            oxigraph::model::Variable::new("__oo_bound_0")?,
                            target.clone(),
                        )
                } else {
                    oxigraph::sparql::SparqlEvaluator::new().parse_query(&text)?
                }
            } else {
                rdf_bindings::prepare(&condition, &values)?
            };
            let fired = ask(prepared)
                .map_err(|e| anyhow::anyhow!("policy {} could not be evaluated: {e}", rule.name))?;
            denied |= fired && rule.effect == "deny";
            results.push(RuleResult {
                rule_name: rule.name.clone(),
                target: target.to_string(),
                effect: rule.effect.clone(),
                fired,
            });
        }
    }
    Ok(PolicyReport {
        verdict: if denied { "deny" } else { "allow" }.into(),
        rule_results: results,
    })
}

/// Check a proposed action against every registered policy rule.
///
/// Semantics:
///   - For each (rule, target) pair, substitute `{target}` into the rule's
///     SPARQL ASK and evaluate.
///   - The rule "fires" iff the ASK returns `true`.
///   - Verdict: `"deny"` if any `deny` rule fires for any target;
///     `"allow"` otherwise.
pub fn check_action(
    db: &StateDb,
    graph: &Arc<GraphStore>,
    target_iris: &[String],
) -> anyhow::Result<PolicyReport> {
    anyhow::ensure!(!target_iris.is_empty(), "target_iris must not be empty");
    let targets = target_iris
        .iter()
        .map(|t| rdf_bindings::iri(t).map(Term::from))
        .collect::<anyhow::Result<Vec<_>>>()?;
    let mut conn = db.conn();
    let metadata = conn.transaction_with_behavior(rusqlite::TransactionBehavior::Immediate)?;
    let rules = read_rules(&metadata)?;
    graph.with_read_transaction(|tx| {
        evaluate(&rules, &targets, |query| {
            match query.on_transaction(tx).execute()? {
                QueryResults::Boolean(b) => Ok(b),
                QueryResults::Solutions(mut rows) => Ok(rows.next().transpose()?.is_some()),
                _ => anyhow::bail!("policy condition must be ASK"),
            }
        })
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::Path;

    fn fresh_db() -> StateDb {
        StateDb::open(Path::new(":memory:")).unwrap()
    }

    fn graph_with_lock() -> Arc<GraphStore> {
        let g = Arc::new(GraphStore::new());
        g.load_turtle(
            r#"
            @prefix owl: <http://www.w3.org/2002/07/owl#> .
            @prefix ex: <http://ex.org/> .
            @prefix audit: <http://example.org/audit#> .
            ex:Critical a owl:Class ; audit:locked "yes" .
            ex:Free a owl:Class .
        "#,
            None,
        )
        .unwrap();
        g
    }

    #[test]
    fn check_action_allows_when_no_deny_fires() {
        let db = fresh_db();
        let g = graph_with_lock();
        register_rule(
            &db,
            &PolicyRule {
                name: "deny_locked".to_string(),
                effect: "deny".to_string(),
                condition: "ASK { <{target}> <http://example.org/audit#locked> \"yes\" }"
                    .to_string(),
                description: None,
            },
        )
        .unwrap();
        let report = check_action(&db, &g, &["http://ex.org/Free".to_string()]).unwrap();
        assert_eq!(report.verdict, "allow");
        // The deny rule did NOT fire for Free.
        assert!(
            report
                .rule_results
                .iter()
                .any(|r| r.rule_name == "deny_locked" && !r.fired)
        );
    }

    #[test]
    fn check_action_denies_when_any_deny_fires() {
        let db = fresh_db();
        let g = graph_with_lock();
        register_rule(
            &db,
            &PolicyRule {
                name: "deny_locked".to_string(),
                effect: "deny".to_string(),
                condition: "ASK { <{target}> <http://example.org/audit#locked> \"yes\" }"
                    .to_string(),
                description: None,
            },
        )
        .unwrap();
        let report = check_action(&db, &g, &["http://ex.org/Critical".to_string()]).unwrap();
        assert_eq!(report.verdict, "deny");
        assert!(
            report
                .rule_results
                .iter()
                .any(|r| r.fired && r.effect == "deny")
        );
    }

    #[test]
    fn register_rule_rejects_invalid_effect() {
        let db = fresh_db();
        let err = register_rule(
            &db,
            &PolicyRule {
                name: "x".to_string(),
                effect: "maybe".to_string(),
                condition: "ASK { ?s ?p ?o }".to_string(),
                description: None,
            },
        )
        .expect_err("should reject");
        assert!(format!("{}", err).contains("invalid effect"));
    }

    #[test]
    fn check_action_with_no_rules_returns_allow() {
        let db = fresh_db();
        let g = graph_with_lock();
        let report = check_action(&db, &g, &["http://ex.org/Free".to_string()]).unwrap();
        assert_eq!(report.verdict, "allow");
        assert!(report.rule_results.is_empty());
    }

    #[test]
    fn target_substitution_replaces_braces() {
        // A rule with multiple target substitutions still works.
        let db = fresh_db();
        let g = graph_with_lock();
        register_rule(
            &db,
            &PolicyRule {
                name: "self_loop_deny".to_string(),
                effect: "deny".to_string(),
                condition: "ASK { <{target}> ?p <{target}> }".to_string(),
                description: None,
            },
        )
        .unwrap();
        // The rule doesn't fire because Critical doesn't have a self-loop.
        let report = check_action(&db, &g, &["http://ex.org/Critical".to_string()]).unwrap();
        assert_eq!(report.verdict, "allow");
    }
}
