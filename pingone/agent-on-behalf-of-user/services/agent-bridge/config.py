import os
import httpx
from dotenv import load_dotenv


load_dotenv()
GC_PROJECT_ID = os.environ["GC_PROJECT_ID"]
GC_REGION = os.environ["GC_REGION"]
CORS_ORIGIN = os.environ["CORS_ORIGIN"]
IDP_ISSUER = os.environ["IDP_ISSUER"].rstrip("/")
JWKS_URI = f"{IDP_ISSUER}/jwks"

_project_number_cache = ""


def _project_number(project_id: str) -> str:
    """Resolve the numeric project number for GC_PROJECT_ID via Cloud Resource Manager.

    Uses the bridge's own Application Default Credentials (Cloud Run service
    account), which can always read its own project's metadata. Cached for the
    process lifetime.
    """
    global _project_number_cache
    if _project_number_cache:
        return _project_number_cache
    import google.auth
    import google.auth.transport.requests as _requests

    creds, _ = google.auth.default(scopes=["https://www.googleapis.com/auth/cloud-platform"])
    auth_req = _requests.Request()
    creds.refresh(auth_req)
    resp = httpx.get(
        "https://cloudresourcemanager.googleapis.com/v1/projects",
        params={"filter": f"projectId:{project_id}"},
        headers={"Authorization": f"Bearer {creds.token}"},
        timeout=10,
    )
    resp.raise_for_status()
    for proj in resp.json().get("projects", []):
        if proj.get("projectId") == project_id:
            _project_number_cache = proj["projectNumber"]
            return _project_number_cache
    raise RuntimeError(f"could not resolve project number for {project_id}")


AGENT_ENGINE_NAME = (
    f"projects/{_project_number(GC_PROJECT_ID)}"
    f"/locations/{GC_REGION}/reasoningEngines/{os.environ['AGENT_ENGINE_ID']}"
)
