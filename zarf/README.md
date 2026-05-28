# zarf/ — Deployment layout for Bodh

> **Attribution.** This directory's layout is adapted from
> [`ardanlabs/service`](https://github.com/ardanlabs/service) by William
> Kennedy / Ardan Labs (Apache 2.0). The Kustomize base/overlay split, the
> `zarf/compose` + `zarf/docker` + `zarf/keys` arrangement, the
> request-equals-limit "Guaranteed QoS" pattern, and the `pg_hba.conf` shape
> for the dev cluster are theirs. Every modification for Bodh's HIPAA-aware,
> multi-agent shape — PodSecurity restricted profile, Workload-Identity
> serviceaccount seam, default-deny NetworkPolicy with provider-tightened
> egress, PDB on the care deployment, distroless final stages — is in this
> repo's history. See [`NOTICE`](../NOTICE) for the full attribution.

---

## Layout

```
zarf/
├── compose/
│   ├── docker_compose.yaml          # local-dev stack (postgres + care + ollama + prom + grafana + tempo + loki)
│   └── pg_hba.conf                  # dev Postgres host-based auth (NOT prod)
├── docker/
│   ├── Dockerfile.care              # cmd/care — HTTP API + clinician SPA
│   ├── Dockerfile.bodhctl           # cmd/bodhctl — operator CLI
│   └── Dockerfile.bodhadmin         # cmd/bodhadmin — admin tool (placeholder)
├── k8s/
│   ├── base/                        # Kustomize base — strict shape
│   │   ├── kustomization.yaml
│   │   ├── namespace.yaml           # PodSecurity = restricted
│   │   ├── serviceaccount.yaml      # Workload Identity seam
│   │   ├── configmap.yaml
│   │   ├── deployment.yaml          # 2 replicas, probes, request==limit
│   │   ├── service.yaml             # ClusterIP only
│   │   ├── networkpolicy.yaml       # default-deny + explicit allow
│   │   └── pdb.yaml                 # minAvailable=1
│   ├── dev/                         # dev overlay — 1 replica, in-cluster Postgres
│   └── prod/                        # prod overlay — 3 replicas, Cloud SQL sidecar
├── keys/
│   ├── README.md                    # how to generate JWT keys
│   └── 54bb2165-...pem.example      # sample placeholder (DO-NOT-USE-IN-PROD)
└── README.md                        # this file
```

The root [`Dockerfile`](../Dockerfile) at the repo root remains the canonical
CI build target — `docker/Dockerfile.care` is a near-identical multistage
build kept here for parity with the Ardan Labs layout and for invocation from
`docker compose build`. The two stay in lockstep; if you change one, change
the other.

## Quickstart paths

| You want… | Run this |
|---|---|
| Local dev — full stack on a laptop | `docker compose -f zarf/compose/docker_compose.yaml up -d` |
| Local dev — K8s (kind / minikube) | `kubectl apply -k zarf/k8s/dev` |
| Production rendering preview | `kubectl kustomize zarf/k8s/prod` |
| Production apply (after secret materialisation) | `kubectl apply -k zarf/k8s/prod` |

Full deployment guide: [`docs/deployment.md`](../docs/deployment.md).

## What ships here vs what doesn't

**Ships here.** Kustomize manifests, docker-compose stack, multistage
Dockerfiles, dev pg_hba, JWT key recipe. Enough to render and apply a
working K8s deployment with `kubectl -k`.

**Does NOT ship here.** Helm charts (the prior Ardan Labs `zarf/helm/`
subtree is not vendored — Kustomize is the project standard), production
secrets, real signing keys, hospital-specific tenant configuration. Those
live in the deployment target's secret store (Secret Manager / Secrets
Manager / Key Vault / Vault) and are pulled in at apply time.

## Drift from upstream

The Ardan Labs original was designed for a single Go service (`sales`) with
an `auth` companion. Bodh is a multi-agent platform where the shipping
binary is `cmd/care`, with `cmd/bodhctl` and a placeholder `cmd/bodhadmin`
companions. The deltas worth flagging when reading this against the
upstream:

| Upstream | Bodh delta |
|---|---|
| `base-sales.yaml` had everything in one file (Namespace + Deployment + Service) | Split into one resource per file; same kustomization assembly |
| Alpine final stage | Distroless static, non-root, readOnlyRootFilesystem |
| No NetworkPolicy in base | Default-deny + explicit allow-list at base |
| No PDB | PDB minAvailable=1 on the care deployment |
| ServiceAccount unannotated | Workload Identity annotation placeholder |
| Single-replica dev pattern | Same in dev overlay; 3 replicas in prod with topology spread |
| pg_hba.conf `trust` everywhere | scram-sha-256 outside the local socket; cert-only auth deferred to Cloud SQL Auth Proxy in prod |

The upstream's Tiltfile and kind-config helpers are not vendored — Bodh's
local-dev story is `docker compose` first, kind second.
