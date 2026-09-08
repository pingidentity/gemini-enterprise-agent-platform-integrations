VENV := .venv
PY   := $(VENV)/bin/python

.PHONY: venv deploy teardown clean

venv:
	python3 -m venv $(VENV)
	$(PY) -m pip install --quiet --upgrade pip
	$(PY) -m pip install --quiet -r requirements.txt

deploy: venv
	@test -f .env || { echo "No .env — copy it from .env.sample first"; exit 1; }
	$(PY) deploy.py

teardown: venv
	@test -f .env || { echo "No .env — copy it from .env.sample first"; exit 1; }
	$(PY) teardown.py

clean:
	rm -rf $(VENV)
