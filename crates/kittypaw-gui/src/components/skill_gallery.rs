use crate::state::AppState;
use dioxus::prelude::*;
use kittypaw_core::package::SkillPackage;
use kittypaw_core::package_manager::PackageManager;

use super::skill_config::SkillConfig;

#[component]
pub fn SkillGallery() -> Element {
    let app_state = use_context::<AppState>();
    let mut packages = use_signal::<Vec<SkillPackage>>(Vec::new);
    let mut selected = use_signal::<Option<SkillPackage>>(|| None);
    let mut filter = use_signal(String::new);
    let mut load_error = use_signal(String::new);

    // Load packages
    use_effect(move || {
        let mgr = PackageManager::new(app_state.packages_dir.clone());
        match mgr.list_installed() {
            Ok(pkgs) => packages.set(pkgs),
            Err(e) => load_error.set(e.to_string()),
        }
    });

    let filtered: Vec<SkillPackage> = packages
        .read()
        .iter()
        .filter(|p| {
            let f = filter.read().to_lowercase();
            f.is_empty()
                || p.meta.name.to_lowercase().contains(&f)
                || p.meta.category.to_lowercase().contains(&f)
        })
        .cloned()
        .collect();

    rsx! {
        div { style: "flex: 1; display: flex; flex-direction: column; overflow: hidden;",

            // Search bar
            div { style: "padding: 12px 16px;",
                input {
                    style: "width: 100%; padding: 10px 14px; border: 1px solid #e2e8f0; border-radius: 10px; font-size: 14px; outline: none; box-sizing: border-box;",
                    placeholder: "Search skills...",
                    value: "{filter}",
                    oninput: move |e| filter.set(e.value()),
                }
            }

            // Content
            div { style: "flex: 1; overflow-y: auto; padding: 0 16px 16px;",
                if !load_error.read().is_empty() {
                    div { style: "text-align: center; padding: 40px; color: #ef4444;",
                        p { "Error: {load_error}" }
                    }
                } else if filtered.is_empty() {
                    div { style: "text-align: center; padding: 40px; color: #64748b;",
                        h2 { style: "font-size: 18px; color: #1e293b;", "No skills found" }
                        p { "Skills will appear here once installed." }
                    }
                } else {
                    div { style: "display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px;",
                        for pkg in filtered.iter() {
                            SkillCard {
                                key: "{pkg.meta.id}",
                                package: pkg.clone(),
                                on_click: move |p: SkillPackage| selected.set(Some(p)),
                            }
                        }
                    }
                }
            }
        }

        if let Some(pkg) = selected.read().as_ref() {
            SkillConfig {
                package: pkg.clone(),
                on_close: move |_| selected.set(None),
            }
        }
    }
}

#[component]
fn SkillCard(package: SkillPackage, on_click: EventHandler<SkillPackage>) -> Element {
    let pkg = package.clone();
    rsx! {
        div {
            style: "border: 1px solid #e2e8f0; border-radius: 12px; padding: 16px; cursor: pointer;",
            onclick: move |_| on_click.call(pkg.clone()),
            div { style: "display: flex; justify-content: space-between; align-items: start; margin-bottom: 8px;",
                h3 { style: "font-size: 15px; font-weight: 600; color: #1e293b; margin: 0;", "{package.meta.name}" }
                span { style: "font-size: 11px; padding: 2px 8px; background: #f1f5f9; border-radius: 12px; color: #64748b;",
                    "{package.meta.category}"
                }
            }
            p { style: "font-size: 13px; color: #64748b; margin: 0; line-height: 1.4;",
                "{package.meta.description}"
            }
        }
    }
}
