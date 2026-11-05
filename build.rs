// SPDX-FileCopyrightText: 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
// SPDX-License-Identifier: MIT
// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
use std::{env, path::Path, process::Command};
fn git(root: &str, args: &[&str]) -> Option<String> {
    let output = Command::new("git")
        .arg("-C")
        .arg(root)
        .args(args)
        .output()
        .ok()?;
    output
        .status
        .success()
        .then(|| String::from_utf8_lossy(&output.stdout).trim().to_owned())
}
fn main() {
    println!("cargo:rerun-if-env-changed=OPEN_ONTOLOGIES_BUILD_VERSION");
    println!("cargo:rerun-if-changed=build.rs");
    println!("cargo:rerun-if-changed=src");
    let base = env::var("CARGO_PKG_VERSION").unwrap();
    let root = env::var("CARGO_MANIFEST_DIR").unwrap();
    let version = env::var("OPEN_ONTOLOGIES_BUILD_VERSION")
        .ok()
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| {
            if git(&root, &["rev-parse", "--show-toplevel"]).as_deref() == Some(root.as_str()) {
                for name in [
                    "HEAD",
                    git(&root, &["symbolic-ref", "--quiet", "HEAD"])
                        .as_deref()
                        .unwrap_or("HEAD"),
                ] {
                    if let Some(path) = git(&root, &["rev-parse", "--git-path", name]) {
                        println!(
                            "cargo:rerun-if-changed={}",
                            Path::new(&root).join(path).display()
                        );
                    }
                }
                if let Some(rev) = git(&root, &["rev-parse", "--verify", "HEAD"]) {
                    assert!(
                        rev.len() == 40 && rev.bytes().all(|c| c.is_ascii_hexdigit()),
                        "invalid source revision"
                    );
                    let dirty = git(&root, &["status", "--porcelain"])
                        .map(|s| !s.is_empty())
                        .unwrap_or(true);
                    return format!(
                        "{base}+local.{}{}",
                        &rev[..12],
                        if dirty { ".dirty" } else { "" }
                    );
                }
            }
            format!("{base}+local.archive")
        });
    let suffix = version
        .strip_prefix(&format!("{base}+local."))
        .expect("build version must retain the upstream version and local marker");
    assert!(!suffix.is_empty() && suffix.split('.').all(|p|!p.is_empty() && p.bytes().all(|c|c.is_ascii_alphanumeric()||c==b'-')),"invalid build metadata");
    println!("cargo:rustc-env=OPEN_ONTOLOGIES_BUILD_VERSION={version}");
}
