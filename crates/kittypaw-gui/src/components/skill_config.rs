use std::collections::HashMap;

use crate::state::AppState;
use dioxus::prelude::*;
use kittypaw_core::package::{ConfigFieldType, SkillPackage};
use kittypaw_core::package_manager::PackageManager;

#[component]
pub fn SkillConfig(package: SkillPackage, on_close: EventHandler) -> Element {
    let app_state = use_context::<AppState>();
    let mut config_values = use_signal::<HashMap<String, String>>(HashMap::new);
    let mut saved = use_signal(|| false);
    let pkg_id = package.meta.id.clone();

    // Load config
    {
        let app_state = app_state.clone();
        let pkg_id = pkg_id.clone();
        use_effect(move || {
            let mgr = PackageManager::new(app_state.packages_dir.clone());
            if let Ok(cfg) = mgr.get_config_with_defaults(&pkg_id) {
                config_values.set(cfg);
            }
        });
    }

    let pkg_id_save = package.meta.id.clone();
    let app_state_save = app_state.clone();

    rsx! {
        div {
            style: "position: fixed; inset: 0; background: rgba(0,0,0,0.4); display: flex; align-items: center; justify-content: center; z-index: 100;",
            onclick: move |_| on_close.call(()),

            div {
                style: "background: #fff; border-radius: 16px; padding: 28px; width: 520px; max-width: 94vw; max-height: 90vh; overflow-y: auto; box-shadow: 0 20px 60px rgba(0,0,0,0.2);",
                onclick: move |e| e.stop_propagation(),

                div { style: "display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px;",
                    h2 { style: "font-size: 18px; font-weight: 600; color: #1e293b; margin: 0;",
                        "{package.meta.name}"
                    }
                    button {
                        style: "background: none; border: none; font-size: 18px; color: #94a3b8; cursor: pointer;",
                        onclick: move |_| on_close.call(()),
                        "X"
                    }
                }

                p { style: "font-size: 13px; color: #64748b; margin-bottom: 20px;",
                    "{package.meta.description}"
                }

                // Config fields
                for field in package.config_schema.iter() {
                    div { style: "margin-bottom: 16px;",
                        label { style: "display: block; font-size: 13px; font-weight: 600; color: #374151; margin-bottom: 4px;",
                            "{field.label}"
                            if field.required {
                                span { style: "color: #ef4444; margin-left: 2px;", " *" }
                            }
                        }
                        if let Some(hint) = &field.hint {
                            p { style: "font-size: 11px; color: #9ca3af; margin: 0 0 6px;", "{hint}" }
                        }
                        {
                            let key = field.key.clone();
                            let current_val = config_values.read().get(&key).cloned().unwrap_or_default();
                            let is_secret = matches!(field.field_type, ConfigFieldType::Secret);
                            rsx! {
                                input {
                                    style: "width: 100%; padding: 8px 12px; border: 1px solid #d1d5db; border-radius: 8px; font-size: 13px; outline: none; box-sizing: border-box;",
                                    r#type: if is_secret { "password" } else { "text" },
                                    value: "{current_val}",
                                    oninput: move |e| {
                                        config_values.write().insert(key.clone(), e.value());
                                    },
                                }
                            }
                        }
                    }
                }

                // Save button
                div { style: "display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px;",
                    button {
                        style: "padding: 10px 24px; background: #2563eb; color: #fff; border: none; border-radius: 8px; font-size: 14px; cursor: pointer;",
                        onclick: {
                            let pkg_id = pkg_id_save.clone();
                            let state = app_state_save.clone();
                            move |_| {
                                let mgr = PackageManager::new(state.packages_dir.clone());
                                for (key, value) in config_values.read().iter() {
                                    let _ = mgr.set_config(&pkg_id, key, value);
                                }
                                saved.set(true);
                            }
                        },
                        if saved() { "Saved" } else { "Save" }
                    }
                }
            }
        }
    }
}
