use std::collections::HashMap;
use std::sync::Arc;

use kittypaw_core::config::ModelConfig;

use crate::claude::ClaudeProvider;
use crate::openai::OpenAiProvider;
use crate::provider::LlmProvider;

pub struct LlmRegistry {
    providers: HashMap<String, Arc<dyn LlmProvider>>,
    default_name: String,
}

impl LlmRegistry {
    pub fn new() -> Self {
        Self {
            providers: HashMap::new(),
            default_name: String::new(),
        }
    }

    /// Register a provider under a name (e.g. "claude-sonnet", "gpt-4o").
    /// The first registered provider becomes the default.
    pub fn register(&mut self, name: &str, provider: Arc<dyn LlmProvider>) {
        if self.default_name.is_empty() {
            self.default_name = name.to_string();
        }
        self.providers.insert(name.to_string(), provider);
    }

    /// Set the default provider name.
    pub fn set_default(&mut self, name: &str) {
        self.default_name = name.to_string();
    }

    /// Get a provider by name.
    pub fn get(&self, name: &str) -> Option<Arc<dyn LlmProvider>> {
        self.providers.get(name).cloned()
    }

    /// Get the default provider.
    pub fn default_provider(&self) -> Option<Arc<dyn LlmProvider>> {
        self.providers.get(&self.default_name).cloned()
    }

    /// List registered provider names.
    pub fn list(&self) -> Vec<String> {
        self.providers.keys().cloned().collect()
    }

    /// Build a registry from model configs.
    /// If api_key is empty in config, tries keychain via kittypaw_core::secrets.
    /// Models without a resolvable API key are skipped.
    pub fn from_configs(configs: &[ModelConfig]) -> Self {
        let mut registry = Self::new();
        for cfg in configs {
            let api_key = if cfg.api_key.is_empty() {
                kittypaw_core::secrets::get_secret("models", &cfg.name)
                    .ok()
                    .flatten()
                    .unwrap_or_default()
            } else {
                cfg.api_key.clone()
            };

            let provider: Arc<dyn LlmProvider> = match cfg.provider.as_str() {
                "claude" | "anthropic" => {
                    if api_key.is_empty() {
                        continue;
                    }
                    Arc::new(ClaudeProvider::new(
                        api_key,
                        cfg.model.clone(),
                        cfg.max_tokens,
                    ))
                }
                "openai" => {
                    if api_key.is_empty() {
                        continue;
                    }
                    if let Some(ref base_url) = cfg.base_url {
                        Arc::new(OpenAiProvider::with_base_url(
                            base_url.clone(),
                            api_key,
                            cfg.model.clone(),
                            cfg.max_tokens,
                        ))
                    } else {
                        Arc::new(OpenAiProvider::new(
                            api_key,
                            cfg.model.clone(),
                            cfg.max_tokens,
                        ))
                    }
                }
                "ollama" | "local" => {
                    let base_url = cfg
                        .base_url
                        .clone()
                        .unwrap_or_else(|| "http://localhost:11434/v1".to_string());
                    Arc::new(OpenAiProvider::with_base_url(
                        base_url,
                        String::new(),
                        cfg.model.clone(),
                        cfg.max_tokens,
                    ))
                }
                _ => continue,
            };

            registry.register(&cfg.name, provider);
            if cfg.default {
                registry.set_default(&cfg.name);
            }
        }
        registry
    }
}

impl Default for LlmRegistry {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use async_trait::async_trait;
    use kittypaw_core::error::Result;
    use kittypaw_core::types::LlmMessage;

    struct MockProvider;

    #[async_trait]
    impl LlmProvider for MockProvider {
        async fn generate(&self, _messages: &[LlmMessage]) -> Result<String> {
            Ok("mock response".into())
        }
    }

    struct MockProviderB;

    #[async_trait]
    impl LlmProvider for MockProviderB {
        async fn generate(&self, _messages: &[LlmMessage]) -> Result<String> {
            Ok("mock response B".into())
        }
    }

    #[test]
    fn test_register_and_get() {
        let mut registry = LlmRegistry::new();
        let provider: Arc<dyn LlmProvider> = Arc::new(MockProvider);
        registry.register("test-model", provider);

        assert!(registry.get("test-model").is_some());
    }

    #[test]
    fn test_default_provider() {
        let mut registry = LlmRegistry::new();
        let provider: Arc<dyn LlmProvider> = Arc::new(MockProvider);
        registry.register("first", provider);

        let provider_b: Arc<dyn LlmProvider> = Arc::new(MockProviderB);
        registry.register("second", provider_b);

        // First registered becomes default
        assert!(registry.default_provider().is_some());
    }

    #[test]
    fn test_set_default() {
        let mut registry = LlmRegistry::new();
        let provider: Arc<dyn LlmProvider> = Arc::new(MockProvider);
        registry.register("first", provider);

        let provider_b: Arc<dyn LlmProvider> = Arc::new(MockProviderB);
        registry.register("second", provider_b);

        registry.set_default("second");
        assert!(registry.default_provider().is_some());
    }

    #[test]
    fn test_list() {
        let mut registry = LlmRegistry::new();
        let provider: Arc<dyn LlmProvider> = Arc::new(MockProvider);
        registry.register("alpha", provider);

        let provider_b: Arc<dyn LlmProvider> = Arc::new(MockProviderB);
        registry.register("beta", provider_b);

        let mut names = registry.list();
        names.sort();
        assert_eq!(names, vec!["alpha", "beta"]);
    }

    #[test]
    fn test_get_nonexistent() {
        let registry = LlmRegistry::new();
        assert!(registry.get("nonexistent").is_none());
    }

    #[test]
    fn test_from_configs_skips_empty_key() {
        let configs = vec![ModelConfig {
            name: "test".into(),
            provider: "claude".into(),
            model: "test-model".into(),
            api_key: String::new(),
            max_tokens: 1024,
            default: false,
            base_url: None,
        }];
        let registry = LlmRegistry::from_configs(&configs);
        assert!(registry.list().is_empty());
    }

    #[test]
    fn test_from_configs_ollama_no_key_needed() {
        let configs = vec![ModelConfig {
            name: "local-qwen".into(),
            provider: "ollama".into(),
            model: "qwen3.5:27b".into(),
            api_key: String::new(),
            max_tokens: 4096,
            default: true,
            base_url: None,
        }];
        let registry = LlmRegistry::from_configs(&configs);
        assert_eq!(registry.list().len(), 1);
        assert!(registry.get("local-qwen").is_some());
    }
}
