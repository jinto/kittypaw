.PHONY: help list contracts-check

help:
	@echo "Targets:"
	@echo "  list             List skeleton files"
	@echo "  contracts-check  Validate JSON contract files with jq"

list:
	@find . -maxdepth 5 -type f | sort

contracts-check:
	@find contracts -name '*.json' -print0 | xargs -0 -n1 jq empty
