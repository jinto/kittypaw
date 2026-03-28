use dioxus::prelude::*;

use super::{chat, settings, sidebar, skill_gallery};

#[component]
pub fn App() -> Element {
    let mut active_tab = use_signal(|| "chat".to_string());
    let mut show_settings = use_signal(|| false);

    rsx! {
        div { class: "app",
            style: "display: flex; height: 100vh; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;",

            sidebar::Sidebar {
                on_tab_change: move |tab: String| active_tab.set(tab),
                on_open_settings: move |_| show_settings.set(true),
            }

            div { class: "main",
                style: "flex: 1; display: flex; flex-direction: column; background: #fff;",

                // Tab bar
                div { class: "tab-bar",
                    style: "display: flex; gap: 2px; padding: 8px 12px 0; background: #f8fafc; border-bottom: 1px solid #e2e8f0;",

                    TabButton { label: "Chat", active: active_tab() == "chat", on_click: move |_| active_tab.set("chat".into()) }
                    TabButton { label: "Skills", active: active_tab() == "skills", on_click: move |_| active_tab.set("skills".into()) }
                }

                // Panel content
                match active_tab().as_str() {
                    "chat" => rsx! { chat::ChatPanel {} },
                    "skills" => rsx! { skill_gallery::SkillGallery {} },
                    _ => rsx! { chat::ChatPanel {} },
                }
            }

            if show_settings() {
                settings::SettingsDialog {
                    on_close: move |_| show_settings.set(false),
                }
            }
        }
    }
}

#[component]
fn TabButton(label: &'static str, active: bool, on_click: EventHandler) -> Element {
    let bg = if active { "#fff" } else { "transparent" };
    let color = if active { "#1e293b" } else { "#64748b" };
    let border = if active { "1px solid #e2e8f0" } else { "none" };

    rsx! {
        button {
            style: "padding: 7px 14px; border: {border}; background: {bg}; color: {color}; font-size: 13px; font-weight: 500; cursor: pointer; border-radius: 7px 7px 0 0;",
            onclick: move |_| on_click.call(()),
            "{label}"
        }
    }
}
