.PHONY: help list contracts-check smoke-local

help:
	@echo "Targets:"
	@echo "  list             List skeleton files"
	@echo "  contracts-check  Validate JSON contract files with jq"
	@echo "  smoke-local      Run repeatable local cross-service smoke"

list:
	@find . -maxdepth 5 -type f | sort

contracts-check:
	@find contracts -name '*.json' -print0 | xargs -0 -n1 jq empty

smoke-local:
	@scripts/smoke-local.sh
