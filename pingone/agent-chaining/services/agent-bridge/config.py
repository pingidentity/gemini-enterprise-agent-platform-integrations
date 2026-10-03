import os
import google.auth
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
AGENT_ENGINE_NAME = (
    f"projects/{GC_PROJECT_ID}"
    f"/locations/{GC_REGION}/reasoningEngines/{os.environ['AGENT_ENGINE_ID']}"
)
