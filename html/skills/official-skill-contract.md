# Official Skill Contract

Official KittyPaw skills must be deterministic executors. They should call APIs,
transform structured data, and format results. They must not interpret arbitrary
user utterances.

## Boundary

- The engine and LLM own natural-language understanding: intent classification,
  slot extraction, disambiguation, and multi-skill chaining.
- Official JavaScript skills own deterministic execution only.
- Runtime code passes structured input through `__context__.params` and stable
  user context through `__context__.user`.

## Rules

- Do not parse `ctx.message.text`, `ctx.input`, or similar raw utterance strings
  in official skills.
- Do not maintain stop-word lists, language-specific particles, regex grammars,
  or prompt-shaped heuristics inside official skills.
- Do not silently fall back from a user-requested explicit slot to a package
  default when the structured slot is missing or invalid.
- Do accept structured values such as `ctx.params.location`, `ctx.params.symbol`,
  `ctx.params.amount`, `ctx.user.location`, and package `ctx.config`.
- If required structured input is missing, return a clear missing-input message
  instead of guessing.
- Do not append KittyPaw brand footers such as `Powered by KittyPaw`.
- Do not show provider/source footers by default. Show provider attribution only
  when package metadata or a runtime API payload says attribution is required.

## Attribution

Official skills should keep answers quiet by default. A user already sees the
answer inside KittyPaw, so repeating `Powered by KittyPaw` in every package
output is noise, not useful provenance.

Use package metadata to describe the durable attribution contract:

```toml
[attribution]
policy = "required-only"

[[attribution.providers]]
id = "provider-id"
name = "Provider Name"
label = "Weather data by Provider Name"
url = "https://provider.example"
required = false
```

When a KittyPaw API proxy knows the upstream license for a particular response,
it may also include runtime metadata:

```json
{
  "attribution": {
    "required": true,
    "label": "Weather data by Provider Name",
    "url": "https://provider.example"
  }
}
```

The package should render the label only when `required` is true. If attribution
is required by the upstream provider, prefer a short plain line such as
`Weather data by Open-Meteo.com`; do not add KittyPaw branding to that line.

## Package Metadata

Each official package should declare the caller-facing contract in
`package.toml`, not in prompt prose hidden inside `main.js`.

Use these sections:

- `[discovery]`: when this package should or should not be selected.
- `[discovery.delegates_to]`: sibling packages for nearby intents, such as
  `now = "weather-now"` or `future = "weather-briefing"`.
- `[capabilities.<slot>]`: structured slots the package can consume and who
  resolves them. Official packages that need natural-language interpretation
  should use `resolution = "engine"`.
- `[invocation]`: responsibilities of the engine/LLM caller, missing-slot
  policy, and post-processing limits.
- `[[invocation.inputs]]`: exact structured paths such as
  `ctx.params.location`, `ctx.params.base`, or `ctx.params.symbols`.
- `[attribution]` and `[[attribution.providers]]`: whether source/provider
  credit is required. Official packages use `policy = "required-only"` unless a
  stronger provider rule is known.

The engine should treat this metadata as the durable contract for LLM-assisted
selection, slot extraction, chaining, and mediation. The JS package should only
execute against `ctx.params`, `ctx.user`, and `ctx.config`.

## Example

For `weather-now`, the chat turn `강남역에 비오나? 지금?` must be handled as:

1. Engine/LLM extracts a structured slot: `location_query = "강남역"`.
2. Engine resolves it to a structured location:
   `{label:"강남역", lat:37.4979, lon:127.0276}`.
3. Engine executes the JS package with:
   `ctx.params.location = {label, lat, lon}`.
4. JS calls the weather API using `lat/lon` and formats the result.

The JS package must not implement Korean natural-language cleanup such as
removing `비오나`, `지금`, particles, or other stop words.
