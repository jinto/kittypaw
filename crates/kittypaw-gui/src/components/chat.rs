use dioxus::prelude::*;
use futures_util::StreamExt;
use kittypaw_core::types::{LlmMessage, Role};

use crate::state::AppState;

#[component]
pub fn ChatPanel() -> Element {
    let app_state = use_context::<AppState>();
    let mut messages = use_signal::<Vec<(String, String)>>(Vec::new);
    let mut input_text = use_signal(String::new);
    let mut is_loading = use_signal(|| false);

    let chat_coroutine = use_coroutine(move |mut rx: UnboundedReceiver<String>| {
        let state = app_state.clone();
        async move {
            while let Some(user_msg) = rx.next().await {
                is_loading.set(true);

                // Get provider from registry
                let provider = {
                    let registry = state.llm_registry.lock().unwrap();
                    registry.default_provider()
                };
                let provider = match provider {
                    Some(p) => p,
                    None => {
                        messages.write().push((
                            "assistant".into(),
                            "No LLM configured. Please set your API key in Settings.".into(),
                        ));
                        is_loading.set(false);
                        continue;
                    }
                };

                // Build conversation history for context
                let mut llm_messages = vec![LlmMessage {
                    role: Role::System,
                    content: "You are KittyPaw, a helpful AI assistant.".into(),
                }];

                // Include previous messages for context
                for (role, content) in messages.read().iter() {
                    let r = match role.as_str() {
                        "user" => Role::User,
                        _ => Role::Assistant,
                    };
                    llm_messages.push(LlmMessage {
                        role: r,
                        content: content.clone(),
                    });
                }

                // Add current user message
                llm_messages.push(LlmMessage {
                    role: Role::User,
                    content: user_msg,
                });

                match provider.generate(&llm_messages).await {
                    Ok(response) => {
                        messages.write().push(("assistant".into(), response));
                    }
                    Err(e) => {
                        messages
                            .write()
                            .push(("assistant".into(), format!("Error: {e}")));
                    }
                }

                is_loading.set(false);
            }
        }
    });

    let mut send_message = move || {
        let msg = input_text.read().clone();
        if msg.is_empty() || *is_loading.read() {
            return;
        }
        messages.write().push(("user".into(), msg.clone()));
        input_text.set(String::new());
        chat_coroutine.send(msg);
    };

    rsx! {
        div { style: "flex: 1; display: flex; flex-direction: column; overflow: hidden;",

            // Messages area
            div { style: "flex: 1; overflow-y: auto; padding: 20px 24px;",
                if messages.read().is_empty() {
                    div { style: "display: flex; flex-direction: column; align-items: center; justify-content: center; height: 100%; text-align: center;",
                        h1 { style: "font-size: 24px; font-weight: 600; color: #1e293b; margin: 0 0 10px;", "How can I help you?" }
                        p { style: "font-size: 15px; color: #64748b;", "I'm KittyPaw, your AI agent. I can run code, automate tasks, and answer questions." }
                    }
                } else {
                    for (i, (role, content)) in messages.read().iter().enumerate() {
                        ChatMessage { key: "{i}", role: role.clone(), content: content.clone() }
                    }
                    if *is_loading.read() {
                        div { style: "display: flex; align-items: center; gap: 8px; color: #64748b; font-size: 13px; padding: 8px 0;",
                            "KittyPaw is thinking..."
                        }
                    }
                }
            }

            // Input area
            div { style: "padding: 12px 16px; border-top: 1px solid #e2e8f0;",
                div { style: "display: flex; gap: 8px;",
                    input {
                        style: "flex: 1; padding: 10px 14px; border: 1px solid #d1d5db; border-radius: 10px; font-size: 14px; outline: none;",
                        placeholder: "Message KittyPaw...",
                        value: "{input_text}",
                        oninput: move |e| input_text.set(e.value()),
                        onkeypress: move |e| {
                            if e.key() == Key::Enter {
                                send_message();
                            }
                        },
                    }
                    {
                        let loading = *is_loading.read();
                        let btn_bg = if loading { "#94a3b8" } else { "#2563eb" };
                        let btn_label = if loading { "..." } else { "Send" };
                        rsx! {
                            button {
                                style: "padding: 10px 16px; background: {btn_bg}; color: #fff; border: none; border-radius: 10px; cursor: pointer; font-size: 14px;",
                                disabled: loading,
                                onclick: move |_| {
                                    send_message();
                                },
                                "{btn_label}"
                            }
                        }
                    }
                }
            }
        }
    }
}

#[component]
fn ChatMessage(role: String, content: String) -> Element {
    let is_user = role == "user";
    let bg = if is_user { "#f1f5f9" } else { "#fff" };
    let align = if is_user { "flex-end" } else { "flex-start" };
    let label = if is_user { "You" } else { "KittyPaw" };
    let label_color = if is_user { "#64748b" } else { "#2563eb" };

    rsx! {
        div { style: "display: flex; flex-direction: column; align-items: {align}; margin-bottom: 16px;",
            span { style: "font-size: 11px; font-weight: 600; color: {label_color}; margin-bottom: 4px;", "{label}" }
            div { style: "max-width: 80%; padding: 10px 14px; background: {bg}; border-radius: 12px; font-size: 14px; color: #1e293b; line-height: 1.5; white-space: pre-wrap;",
                "{content}"
            }
        }
    }
}
