use std::collections::HashMap;
use std::sync::Arc;

use crate::state::AppState;
use dioxus::prelude::*;
use kittypaw_core::config::SandboxConfig;
use kittypaw_core::package::{ConfigField, ConfigFieldType, SkillPackage};
use kittypaw_core::package_manager::PackageManager;

// ---------------------------------------------------------------------------
// Wizard step model
// ---------------------------------------------------------------------------

#[derive(Clone, PartialEq)]
enum WizardStep {
    Overview,
    /// A single required config field (index into `config_schema`).
    Field(usize),
    /// All optional config fields grouped together.
    OptionalFields,
    /// LLM model override selection.
    Model,
    /// Summary + Test Run + Save.
    Review,
}

fn build_steps(schema: &[ConfigField], has_models: bool) -> Vec<WizardStep> {
    let mut steps = vec![WizardStep::Overview];

    // Required fields: one step each
    for (i, field) in schema.iter().enumerate() {
        if field.required {
            steps.push(WizardStep::Field(i));
        }
    }

    // Optional fields: grouped into one step
    if schema.iter().any(|f| !f.required) {
        steps.push(WizardStep::OptionalFields);
    }

    if has_models {
        steps.push(WizardStep::Model);
    }

    steps.push(WizardStep::Review);
    steps
}

// ---------------------------------------------------------------------------
// Channel keys that can be auto-filled from global Settings
// ---------------------------------------------------------------------------

const CHANNEL_KEYS: &[&str] = &["telegram_token", "chat_id"];
const CALLOUT_STYLE: &str = "background: #F0FDF4; border-left: 3px solid #86EFAC; padding: 12px 16px; border-radius: 0 8px 8px 0;";

fn get_auto_filled(key: &str) -> Option<String> {
    if CHANNEL_KEYS.contains(&key) {
        if let Ok(Some(val)) = kittypaw_core::secrets::get_secret("channels", key) {
            return Some(val);
        }
    }
    None
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

#[component]
pub fn SkillConfig(package: SkillPackage, on_close: EventHandler) -> Element {
    let app_state = use_context::<AppState>();
    let mut config_values = use_signal::<HashMap<String, String>>(HashMap::new);
    let mut test_output = use_signal(String::new);
    let mut testing = use_signal(|| false);
    let mut saving = use_signal(|| false);
    let pkg_id = package.meta.id.clone();

    // Load existing config
    {
        let app_state = app_state.clone();
        let pkg_id = pkg_id.clone();
        let schema = package.config_schema.clone();
        use_effect(move || {
            let mgr = PackageManager::new(app_state.packages_dir.clone());
            let mut vals = mgr.get_config_with_defaults(&pkg_id).unwrap_or_default();

            // Auto-fill channel keys from global settings if not already set
            for field in &schema {
                if !vals.contains_key(&field.key) || vals[&field.key].is_empty() {
                    if let Some(auto) = get_auto_filled(&field.key) {
                        vals.insert(field.key.clone(), auto);
                    }
                }
            }
            config_values.set(vals);
        });
    }

    // Compute once: model names (avoids mutex lock per render)
    let model_names = use_signal({
        let registry = app_state.llm_registry.clone();
        move || registry.lock().map(|r| r.list()).unwrap_or_default()
    });

    // Compute once: which keys are auto-filled from global Settings (avoids keychain read per render)
    let auto_filled_keys = use_signal({
        let schema = package.config_schema.clone();
        move || {
            schema
                .iter()
                .filter(|f| get_auto_filled(&f.key).is_some())
                .map(|f| f.key.clone())
                .collect::<Vec<_>>()
        }
    });

    let steps = build_steps(&package.config_schema, !model_names().is_empty());
    let total_steps = steps.len();

    let step_idx = use_signal(|| 0usize);

    // Clamp step index
    let current_step = {
        let idx = step_idx();
        if idx >= total_steps {
            steps.last().cloned().unwrap_or(WizardStep::Review)
        } else {
            steps[idx].clone()
        }
    };

    // can_proceed: Field(i) is only created for required fields, so just check non-empty
    let can_proceed = {
        let vals = config_values.read();
        match &current_step {
            WizardStep::Field(i) => {
                let key = &package.config_schema[*i].key;
                !vals.get(key).map(|v| v.is_empty()).unwrap_or(true)
            }
            _ => true,
        }
    };

    let pkg_id_save = package.meta.id.clone();
    let app_state_save = app_state.clone();
    let pkg_id_test = package.meta.id.clone();
    let app_state_test = app_state.clone();

    rsx! {
        div {
            style: "position: fixed; inset: 0; background: rgba(0,0,0,0.4); display: flex; align-items: center; justify-content: center; z-index: 100;",
            onclick: move |_| on_close.call(()),

            div {
                style: "background: #fff; border-radius: 16px; padding: 32px; width: 520px; max-width: 94vw; max-height: 85vh; overflow-y: auto; box-shadow: 0 20px 60px rgba(0,0,0,0.2);",
                onclick: move |e| e.stop_propagation(),

                // Header
                div { style: "display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;",
                    h2 { style: "font-family: 'Fraunces', Georgia, serif; font-size: 20px; font-weight: 700; color: #1C1917; margin: 0;",
                        "{package.meta.name}"
                    }
                    button {
                        style: "background: none; border: none; font-size: 18px; color: #94a3b8; cursor: pointer;",
                        onclick: move |_| on_close.call(()),
                        "X"
                    }
                }

                // Progress dots
                WizardDots { current: step_idx(), total: total_steps }

                // Step content
                div { style: "margin-top: 24px; min-height: 200px;",
                    match &current_step {
                        WizardStep::Overview => rsx! {
                            WizardStepOverview { package: package.clone() }
                        },
                        WizardStep::Field(i) => rsx! {
                            WizardStepField {
                                field: package.config_schema[*i].clone(),
                                value: config_values.read().get(&package.config_schema[*i].key).cloned().unwrap_or_default(),
                                is_auto_filled: auto_filled_keys().contains(&package.config_schema[*i].key),
                                on_change: {
                                    let key = package.config_schema[*i].key.clone();
                                    move |val: String| {
                                        config_values.write().insert(key.clone(), val);
                                    }
                                },
                            }
                        },
                        WizardStep::OptionalFields => rsx! {
                            WizardStepOptional {
                                fields: package.config_schema.iter().filter(|f| !f.required).cloned().collect::<Vec<_>>(),
                                config_values: config_values,
                            }
                        },
                        WizardStep::Model => rsx! {
                            WizardStepModel {
                                model_names: model_names(),
                                current_model: config_values.read().get("_model").cloned().unwrap_or_default(),
                                on_change: move |val: String| {
                                    if val.is_empty() {
                                        config_values.write().remove("_model");
                                    } else {
                                        config_values.write().insert("_model".to_string(), val);
                                    }
                                },
                            }
                        },
                        WizardStep::Review => rsx! {
                            WizardStepReview {
                                package: package.clone(),
                                config_values: config_values,
                                test_output: test_output,
                                testing: testing,
                                saving: saving,
                                on_save: {
                                    let pkg_id = pkg_id_save.clone();
                                    let state = app_state_save.clone();
                                    move |_| {
                                        let mgr = PackageManager::new(state.packages_dir.clone());
                                        for (key, value) in config_values.read().iter() {
                                            let _ = mgr.set_config(&pkg_id, key, value);
                                        }
                                        saving.set(true);
                                    }
                                },
                                on_test: {
                                    let pkg_id = pkg_id_test.clone();
                                    let state = app_state_test.clone();
                                    move |_| {
                                        testing.set(true);
                                        test_output.set(String::new());

                                        let pkg_id = pkg_id.clone();
                                        let packages_dir = state.packages_dir.clone();
                                        let config = config_values.read().clone();
                                        let store = state.store.clone();

                                        spawn(async move {
                                            let result = run_skill_test(pkg_id, packages_dir, config, store).await;
                                            test_output.set(result);
                                            testing.set(false);
                                        });
                                    }
                                },
                                on_close: on_close,
                            }
                        },
                    }
                }

                // Navigation
                if !matches!(current_step, WizardStep::Review) {
                    WizardNav {
                        step_idx: step_idx,
                        total: total_steps,
                        can_proceed: can_proceed,
                    }
                }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Progress dots
// ---------------------------------------------------------------------------

#[component]
fn WizardDots(current: usize, total: usize) -> Element {
    let dots: Vec<String> = (0..total)
        .map(|i| {
            let color = if i == current {
                "#86EFAC"
            } else if i < current {
                "#166534"
            } else {
                "#E7E5E4"
            };
            format!("width: 8px; height: 8px; border-radius: 50%; background: {color}; transition: background 150ms;")
        })
        .collect();

    rsx! {
        div { style: "display: flex; gap: 6px; justify-content: center; margin-top: 12px;",
            for dot_style in dots.iter() {
                div { style: "{dot_style}" }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Navigation bar
// ---------------------------------------------------------------------------

#[component]
fn WizardNav(step_idx: Signal<usize>, total: usize, can_proceed: bool) -> Element {
    let at_start = step_idx() == 0;
    let at_last_before_review = step_idx() + 1 >= total - 1;

    rsx! {
        div { style: "display: flex; justify-content: space-between; align-items: center; margin-top: 28px;",
            // Back button
            if !at_start {
                button {
                    style: "padding: 10px 24px; background: transparent; color: #1C1917; border: 1px solid #E7E5E4; border-radius: 8px; font-size: 14px; cursor: pointer;",
                    onclick: move |_| step_idx.set(step_idx() - 1),
                    "이전"
                }
            } else {
                div {}
            }

            // Next button
            button {
                style: if can_proceed {
                    "padding: 10px 24px; background: #86EFAC; color: #166534; border: none; border-radius: 8px; font-size: 14px; font-weight: 600; cursor: pointer;"
                } else {
                    "padding: 10px 24px; background: #E7E5E4; color: #78716C; border: none; border-radius: 8px; font-size: 14px; font-weight: 600; cursor: not-allowed;"
                },
                disabled: !can_proceed,
                onclick: move |_| {
                    if can_proceed {
                        step_idx.set(step_idx() + 1);
                    }
                },
                if at_last_before_review { "확인" } else { "다음" }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Step: Overview
// ---------------------------------------------------------------------------

#[component]
fn WizardStepOverview(package: SkillPackage) -> Element {
    rsx! {
        div {
            p { style: "font-size: 14px; color: #57534E; line-height: 1.6; margin: 0 0 20px;",
                "{package.meta.description}"
            }

            if let Some(notes) = &package.meta.setup_notes {
                div {
                    style: "{CALLOUT_STYLE} margin-bottom: 20px;",
                    p { style: "font-size: 13px; color: #166534; margin: 0; line-height: 1.5; white-space: pre-line;",
                        "{notes}"
                    }
                }
            }

            // Permissions
            if !package.permissions.primitives.is_empty() {
                div { style: "margin-bottom: 8px;",
                    label { style: "display: block; font-size: 12px; font-weight: 600; color: #78716C; margin-bottom: 8px;",
                        "사용 권한"
                    }
                    div { style: "display: flex; flex-wrap: wrap; gap: 6px;",
                        for prim in package.permissions.primitives.iter() {
                            span {
                                style: "padding: 4px 10px; background: #F5F3F0; color: #57534E; border-radius: 4px; font-size: 12px; font-weight: 500;",
                                "{prim}"
                            }
                        }
                    }
                }
            }

            if package.config_schema.is_empty() {
                p { style: "font-size: 13px; color: #78716C; margin-top: 16px;",
                    "이 스킬은 별도 설정이 필요 없습니다."
                }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Step: Single required field
// ---------------------------------------------------------------------------

#[component]
fn WizardStepField(
    field: ConfigField,
    value: String,
    is_auto_filled: bool,
    on_change: EventHandler<String>,
) -> Element {
    rsx! {
        div {
            // Label
            div { style: "display: flex; align-items: center; gap: 8px; margin-bottom: 8px;",
                label { style: "font-size: 15px; font-weight: 600; color: #1C1917;",
                    "{field.label}"
                }
                span { style: "font-size: 11px; color: #ef4444; font-weight: 500;", "필수" }
                if is_auto_filled {
                    span { style: "font-size: 11px; color: #166534; background: #F0FDF4; padding: 2px 8px; border-radius: 4px; font-weight: 500;",
                        "Settings 자동 입력"
                    }
                }
            }

            if let Some(guide) = &field.setup_guide {
                div {
                    style: "{CALLOUT_STYLE} margin-bottom: 16px;",
                    p { style: "font-size: 13px; color: #166534; margin: 0; line-height: 1.6; white-space: pre-line;",
                        "{guide}"
                    }
                }
            }

            // Hint
            if let Some(hint) = &field.hint {
                p { style: "font-size: 12px; color: #9ca3af; margin: 0 0 8px;", "{hint}" }
            }

            // Input
            FieldInput { field: field.clone(), value: value, on_change: on_change }
        }
    }
}

// ---------------------------------------------------------------------------
// Step: Optional fields (grouped)
// ---------------------------------------------------------------------------

#[component]
fn WizardStepOptional(
    fields: Vec<ConfigField>,
    config_values: Signal<HashMap<String, String>>,
) -> Element {
    rsx! {
        div {
            h3 { style: "font-size: 15px; font-weight: 600; color: #1C1917; margin: 0 0 4px;",
                "선택 설정"
            }
            p { style: "font-size: 12px; color: #78716C; margin: 0 0 16px;",
                "기본값이 설정되어 있어 변경하지 않아도 됩니다."
            }

            for field in fields.iter() {
                div { style: "margin-bottom: 16px;",
                    label { style: "display: block; font-size: 13px; font-weight: 600; color: #374151; margin-bottom: 4px;",
                        "{field.label}"
                    }
                    if let Some(hint) = &field.hint {
                        p { style: "font-size: 11px; color: #9ca3af; margin: 0 0 6px;", "{hint}" }
                    }
                    {
                        let key = field.key.clone();
                        let current_val = config_values.read().get(&key).cloned().unwrap_or_default();
                        rsx! {
                            FieldInput {
                                field: field.clone(),
                                value: current_val,
                                on_change: move |val: String| {
                                    config_values.write().insert(key.clone(), val);
                                },
                            }
                        }
                    }
                }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Step: Model selection
// ---------------------------------------------------------------------------

#[component]
fn WizardStepModel(
    model_names: Vec<String>,
    current_model: String,
    on_change: EventHandler<String>,
) -> Element {
    rsx! {
        div {
            h3 { style: "font-size: 15px; font-weight: 600; color: #1C1917; margin: 0 0 4px;",
                "모델 설정"
            }
            p { style: "font-size: 12px; color: #78716C; margin: 0 0 16px;",
                "이 스킬의 LLM 호출에 사용할 모델을 선택하세요. 비워두면 기본 모델을 사용합니다."
            }
            select {
                style: "width: 100%; padding: 10px 14px; border: 1px solid #E7E5E4; border-radius: 8px; font-size: 14px; outline: none; box-sizing: border-box; background: #fff; color: #1C1917;",
                value: "{current_model}",
                onchange: move |e| on_change.call(e.value()),
                option { value: "", "Default" }
                for name in &model_names {
                    option {
                        value: "{name}",
                        selected: *name == current_model,
                        "{name}"
                    }
                }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Step: Review + Test Run + Save
// ---------------------------------------------------------------------------

#[component]
fn WizardStepReview(
    package: SkillPackage,
    config_values: Signal<HashMap<String, String>>,
    test_output: Signal<String>,
    testing: Signal<bool>,
    saving: Signal<bool>,
    on_save: EventHandler,
    on_test: EventHandler,
    on_close: EventHandler,
) -> Element {
    let vals = config_values.read();

    rsx! {
        div {
            h3 { style: "font-size: 15px; font-weight: 600; color: #1C1917; margin: 0 0 16px;",
                "설정 확인"
            }

            // Summary table
            div { style: "border: 1px solid #E7E5E4; border-radius: 8px; overflow: hidden; margin-bottom: 20px;",
                for (idx, field) in package.config_schema.iter().enumerate() {
                    {
                        let val = vals.get(&field.key).cloned().unwrap_or_default();
                        let display_val = if val.is_empty() {
                            "(기본값)".to_string()
                        } else if matches!(field.field_type, ConfigFieldType::Secret) {
                            "\u{2022}\u{2022}\u{2022}\u{2022}\u{2022}\u{2022}".to_string()
                        } else {
                            val
                        };
                        let is_empty = vals.get(&field.key).map(|v| v.is_empty()).unwrap_or(true);
                        let border_top = if idx > 0 { "border-top: 1px solid #E7E5E4;" } else { "" };

                        rsx! {
                            div {
                                style: "display: flex; justify-content: space-between; padding: 10px 14px; {border_top}",
                                span { style: "font-size: 13px; color: #57534E; font-weight: 500;",
                                    "{field.label}"
                                }
                                span {
                                    style: if is_empty {
                                        "font-size: 13px; color: #9ca3af; font-style: italic;"
                                    } else {
                                        "font-size: 13px; color: #1C1917; font-family: 'SF Mono', 'Fira Code', monospace;"
                                    },
                                    "{display_val}"
                                }
                            }
                        }
                    }
                }

                // Model row
                {
                    let model_val = vals.get("_model").cloned().unwrap_or_default();
                    let display = if model_val.is_empty() { "Default".to_string() } else { model_val };
                    let border_top = if !package.config_schema.is_empty() { "border-top: 1px solid #E7E5E4;" } else { "" };
                    rsx! {
                        div {
                            style: "display: flex; justify-content: space-between; padding: 10px 14px; {border_top}",
                            span { style: "font-size: 13px; color: #57534E; font-weight: 500;", "Model" }
                            span { style: "font-size: 13px; color: #1C1917;", "{display}" }
                        }
                    }
                }
            }

            // Action buttons
            div { style: "display: flex; justify-content: flex-end; gap: 8px;",
                // Test Run
                {
                    let is_testing = testing();
                    rsx! {
                        button {
                            style: if is_testing {
                                "padding: 10px 24px; background: #94a3b8; color: #fff; border: none; border-radius: 8px; font-size: 14px; cursor: wait;"
                            } else {
                                "padding: 10px 24px; background: #059669; color: #fff; border: none; border-radius: 8px; font-size: 14px; cursor: pointer;"
                            },
                            disabled: is_testing,
                            onclick: move |_| on_test.call(()),
                            if is_testing { "실행 중..." } else { "테스트" }
                        }
                    }
                }

                // Save & Close
                button {
                    style: "padding: 10px 24px; background: #86EFAC; color: #166534; border: none; border-radius: 8px; font-size: 14px; font-weight: 600; cursor: pointer;",
                    onclick: move |_| {
                        on_save.call(());
                        on_close.call(());
                    },
                    if saving() { "저장됨" } else { "저장" }
                }
            }

            // Test output
            if !test_output.read().is_empty() || testing() {
                div { style: "margin-top: 16px; padding: 12px; background: #F5F3F0; border: 1px solid #E7E5E4; border-radius: 8px;",
                    label { style: "display: block; font-size: 12px; font-weight: 600; color: #475569; margin-bottom: 6px;",
                        "Test Output"
                    }
                    if testing() {
                        p { style: "font-size: 13px; color: #64748b; margin: 0;", "스킬을 실행하는 중..." }
                    } else {
                        pre { style: "font-size: 12px; color: #1e293b; margin: 0; white-space: pre-wrap; word-break: break-all; font-family: 'SF Mono', 'Fira Code', monospace;",
                            "{test_output}"
                        }
                    }
                }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Reusable field input renderer
// ---------------------------------------------------------------------------

#[component]
fn FieldInput(field: ConfigField, value: String, on_change: EventHandler<String>) -> Element {
    let input_style = "width: 100%; padding: 10px 14px; border: 1px solid #E7E5E4; border-radius: 8px; font-size: 14px; outline: none; box-sizing: border-box; background: #fff; color: #1C1917;";

    match field.field_type {
        ConfigFieldType::Secret => rsx! {
            input {
                style: "{input_style}",
                r#type: "password",
                value: "{value}",
                oninput: move |e| on_change.call(e.value()),
            }
        },
        ConfigFieldType::Number => rsx! {
            input {
                style: "{input_style}",
                r#type: "number",
                value: "{value}",
                oninput: move |e| on_change.call(e.value()),
            }
        },
        ConfigFieldType::Boolean => {
            let checked = value == "true";
            rsx! {
                label { style: "display: flex; align-items: center; gap: 8px; cursor: pointer;",
                    input {
                        r#type: "checkbox",
                        checked: checked,
                        onchange: move |e: Event<FormData>| {
                            on_change.call(if e.value() == "true" { "true".into() } else { "false".into() });
                        },
                    }
                    span { style: "font-size: 14px; color: #1C1917;",
                        if checked { "On" } else { "Off" }
                    }
                }
            }
        }
        ConfigFieldType::Select => {
            let options = field.options.clone().unwrap_or_default();
            rsx! {
                select {
                    style: "{input_style}",
                    value: "{value}",
                    onchange: move |e| on_change.call(e.value()),
                    option { value: "", "선택하세요" }
                    for opt in &options {
                        option {
                            value: "{opt}",
                            selected: *opt == value,
                            "{opt}"
                        }
                    }
                }
            }
        }
        ConfigFieldType::Cron => rsx! {
            input {
                style: "{input_style} font-family: 'SF Mono', 'Fira Code', monospace;",
                r#type: "text",
                placeholder: "0 7 * * *",
                value: "{value}",
                oninput: move |e| on_change.call(e.value()),
            }
        },
        ConfigFieldType::String => rsx! {
            input {
                style: "{input_style}",
                r#type: "text",
                value: "{value}",
                oninput: move |e| on_change.call(e.value()),
            }
        },
    }
}

// ---------------------------------------------------------------------------
// Test runner (unchanged from original)
// ---------------------------------------------------------------------------

async fn run_skill_test(
    pkg_id: String,
    packages_dir: std::path::PathBuf,
    config: HashMap<String, String>,
    store: Arc<tokio::sync::Mutex<kittypaw_store::Store>>,
) -> String {
    let js_path = packages_dir.join(&pkg_id).join("main.js");
    let js_code = match std::fs::read_to_string(&js_path) {
        Ok(code) => code,
        Err(e) => return format!("Error reading main.js: {e}"),
    };

    let sandbox = kittypaw_sandbox::Sandbox::new_threaded(SandboxConfig {
        timeout_secs: 30,
        memory_limit_mb: 128,
        allowed_paths: vec![],
        allowed_hosts: vec![],
    });

    let config_for_resolver = kittypaw_core::config::Config::default();
    let store_for_resolver = store.clone();
    let resolver: Option<kittypaw_sandbox::SkillResolver> = Some(Arc::new(move |call| {
        let store = store_for_resolver.clone();
        let config = config_for_resolver.clone();
        Box::pin(async move {
            kittypaw_cli::skill_executor::resolve_skill_call(&call, &config, &store, None, None)
                .await
        })
    }));

    let context = serde_json::json!({
        "config": config,
        "package_id": pkg_id,
        "user": {},
    });
    let wrapped = format!("const ctx = JSON.parse(__context__);\n{js_code}");

    match sandbox
        .execute_with_resolver(&wrapped, context, resolver)
        .await
    {
        Ok(result) => {
            if result.success {
                if result.output.is_empty() {
                    "(no output)".into()
                } else {
                    result.output
                }
            } else {
                format!("Error: {}", result.error.unwrap_or_default())
            }
        }
        Err(e) => format!("Execution error: {e}"),
    }
}
