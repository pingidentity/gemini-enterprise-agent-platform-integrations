import os
import httpx
import google.auth
import google.auth.transport.requests
from typing import Any
from dotenv import load_dotenv

load_dotenv()

GC_PROJECT_ID = google.auth.default()[1]
if not GC_PROJECT_ID:
    raise RuntimeError("could not resolve the GCP project ID from Application Default Credentials")
GC_REGION = os.environ["GC_REGION"]
CORS_ORIGIN = os.environ["CORS_ORIGIN"]
IDP_ISSUER = os.environ["IDP_ISSUER"].rstrip("/")
IDP_REQUIRED_AUDIENCE = os.environ["IDP_REQUIRED_AUDIENCE"]
JWKS_URI = f"{IDP_ISSUER}/jwks"
_project_number_cache: str = ""


def _project_number(project_id: str) -> str:
    """Resolve the numeric project number via Cloud Resource Manager (ADC-authed, cached)."""
    global _project_number_cache
    if _project_number_cache:
        return _project_number_cache
    creds, _ = google.auth.default(scopes=["https://www.googleapis.com/auth/cloud-platform"])
    creds.refresh(google.auth.transport.requests.Request())
    resp = httpx.get(
        "https://cloudresourcemanager.googleapis.com/v1/projects",
        params={"filter": f"projectId:{project_id}"},
        headers={"Authorization": f"Bearer {creds.token}"},
        timeout=10,
    )
    resp.raise_for_status()
    projects: list[dict[str, Any]] = resp.json().get("projects", [])
    for proj in projects:
        if proj.get("projectId") == project_id:
            _project_number_cache = proj["projectNumber"]
            return _project_number_cache
    raise RuntimeError(f"could not resolve project number for {project_id}")


AGENT_ENGINE_NAME = (
    f"projects/{_project_number(GC_PROJECT_ID)}"
    f"/locations/{GC_REGION}/reasoningEngines/{os.environ['AGENT_ENGINE_ID']}"
)
