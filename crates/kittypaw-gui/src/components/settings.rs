use std::sync::Arc;

use dioxus::prelude::*;
use kittypaw_llm::claude::ClaudeProvider;
use kittypaw_llm::openai::OpenAiProvider;

use crate::state::AppState;

#[component]
pub fn SettingsDialog(on_close: EventHandler) -> Element {
    let app_state = use_context::<AppState>();
    let mut api_key = use_signal(String::new);
    let mut saved = use_signal(|| false);

    // Local model signals
    let mut local_url = use_signal(String::new);
    let mut local_model = use_signal(String::new);
    let mut local_saved = use_signal(|| false);

    // Load current key on mount
    {
        let app_state = app_state.clone();
        use_effect(move || {
            let key = app_state.api_key.lock().unwrap().clone();
            if !key.is_empty() {
                let len = key.len();
                let suffix = &key[len.saturating_sub(4)..];
                api_key.set(format!("sk-...{suffix}"));
            }

            // Load stored local model config
            if let Ok(Some(url)) = kittypaw_core::secrets::get_secret("local_model", "base_url") {
                local_url.set(url);
            }
            if let Ok(Some(model)) = kittypaw_core::secrets::get_secret("local_model", "model_name")
            {
                local_model.set(model);
            }
        });
    }

    let app_state_save = app_state.clone();
    let app_state_local_save = app_state.clone();

    rsx! {
        // Overlay
        div {
            style: "position: fixed; inset: 0; background: rgba(0,0,0,0.4); display: flex; align-items: center; justify-content: center; z-index: 100;",
            onclick: move |_| on_close.call(()),

            // Panel (stop propagation)
            div {
                style: "background: #fff; border-radius: 16px; padding: 28px; width: 520px; max-width: 94vw; box-shadow: 0 20px 60px rgba(0,0,0,0.2);",
                onclick: move |e| e.stop_propagation(),

                div { style: "display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px;",
                    h2 { style: "font-size: 18px; font-weight: 600; color: #1e293b; margin: 0;", "Settings" }
                    button {
                        style: "background: none; border: none; font-size: 18px; color: #94a3b8; cursor: pointer;",
                        onclick: move |_| on_close.call(()),
                        "X"
                    }
                }

                // ── Anthropic API Key section ──
                div { style: "margin-bottom: 8px;",
                    div { style: "display: flex; align-items: center; gap: 8px; margin-bottom: 16px;",
                        div { style: "flex: 1; height: 1px; background: #e5e7eb;" }
                        span { style: "font-size: 12px; font-weight: 600; color: #6b7280; white-space: nowrap;", "Anthropic API Key" }
                        div { style: "flex: 1; height: 1px; background: #e5e7eb;" }
                    }
                    p { style: "font-size: 12px; color: #6b7280; margin-bottom: 8px;", "Your API key is stored locally." }
                    input {
                        style: "width: 100%; padding: 10px 12px; border: 1px solid #d1d5db; border-radius: 8px; font-size: 14px; font-family: monospace; outline: none; box-sizing: border-box;",
                        r#type: "password",
                        placeholder: "sk-ant-...",
                        value: "{api_key}",
                        oninput: move |e| api_key.set(e.value()),
                    }
                }

                div { style: "display: flex; justify-content: flex-end; margin-bottom: 24px;",
                    button {
                        style: "padding: 10px 24px; background: #2563eb; color: #fff; border: none; border-radius: 8px; font-size: 14px; cursor: pointer;",
                        onclick: {
                            let state = app_state_save.clone();
                            move |_| {
                                let key = api_key.read().clone();
                                // Skip masked keys
                                if !key.starts_with("sk-...") {
                                    let _ = kittypaw_core::secrets::set_secret("settings", "api_key", &key);
                                    *state.api_key.lock().unwrap() = key.clone();
                                    let mut registry = state.llm_registry.lock().unwrap();
                                    registry.register(
                                        "claude-sonnet",
                                        Arc::new(ClaudeProvider::new(
                                            key,
                                            "claude-sonnet-4-20250514".into(),
                                            4096,
                                        )),
                                    );
                                }
                                saved.set(true);
                            }
                        },
                        if saved() { "Saved" } else { "Save" }
                    }
                }

                // ── 로컬 모델 연결 section ──
                div { style: "margin-bottom: 16px;",
                    div { style: "display: flex; align-items: center; gap: 8px; margin-bottom: 16px;",
                        div { style: "flex: 1; height: 1px; background: #e5e7eb;" }
                        span { style: "font-size: 12px; font-weight: 600; color: #6b7280; white-space: nowrap;", "로컬 모델 연결" }
                        div { style: "flex: 1; height: 1px; background: #e5e7eb;" }
                    }

                    div { style: "margin-bottom: 12px;",
                        label { style: "display: block; font-size: 13px; font-weight: 600; color: #374151; margin-bottom: 6px;", "모델 서버 URL" }
                        input {
                            style: "width: 100%; padding: 10px 12px; border: 1px solid #d1d5db; border-radius: 8px; font-size: 14px; outline: none; box-sizing: border-box;",
                            r#type: "text",
                            placeholder: "http://localhost:11434/v1",
                            value: "{local_url}",
                            oninput: move |e| local_url.set(e.value()),
                        }
                    }

                    div { style: "margin-bottom: 12px;",
                        label { style: "display: block; font-size: 13px; font-weight: 600; color: #374151; margin-bottom: 6px;", "모델 이름" }
                        input {
                            style: "width: 100%; padding: 10px 12px; border: 1px solid #d1d5db; border-radius: 8px; font-size: 14px; outline: none; box-sizing: border-box;",
                            r#type: "text",
                            placeholder: "qwen3.5:27b",
                            value: "{local_model}",
                            oninput: move |e| local_model.set(e.value()),
                        }
                    }

                    div { style: "display: flex; justify-content: flex-end; margin-bottom: 12px;",
                        button {
                            style: "padding: 10px 24px; background: #2563eb; color: #fff; border: none; border-radius: 8px; font-size: 14px; cursor: pointer;",
                            onclick: {
                                let state = app_state_local_save.clone();
                                move |_| {
                                    let url = local_url.read().clone();
                                    let model = local_model.read().clone();
                                    if !url.is_empty() && !model.is_empty() {
                                        let _ = kittypaw_core::secrets::set_secret("local_model", "base_url", &url);
                                        let _ = kittypaw_core::secrets::set_secret("local_model", "model_name", &model);
                                        let mut registry = state.llm_registry.lock().unwrap();
                                        registry.register(
                                            "local",
                                            Arc::new(OpenAiProvider::with_base_url(
                                                url,
                                                String::new(),
                                                model,
                                                4096,
                                            )),
                                        );
                                        registry.set_default("local");
                                    }
                                    local_saved.set(true);
                                }
                            },
                            if local_saved() { "저장 완료" } else { "저장" }
                        }
                    }

                    p { style: "font-size: 12px; color: #6b7280; line-height: 1.5;",
                        "Ollama, LM Studio 등 OpenAI 호환 API 서버를 연결합니다."
                        br {}
                        "로컬 모델 연결 시 API 키 없이 무료로 사용할 수 있습니다."
                    }
                }
            }
        }
    }
}
