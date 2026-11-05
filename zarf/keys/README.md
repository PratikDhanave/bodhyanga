# zarf/keys — JWT signing keys

> Adapted from the Ardan Labs `service` layout
> ([`zarf/keys/`](https://github.com/ardanlabs/service/tree/master/zarf/keys)).
> Original copyright Ardan Labs (Apache 2.0). Modifications for Bodhyanga.

This directory holds **sample** RS256 / EdDSA JWT signing keys used by Bodhyanga
when running locally (compose, kind, dev overlay). The shipped key file is
clearly marked `DO-NOT-USE-IN-PROD` and is rotated only as a placeholder.

**Never check a production key into this repo.** Production keys live in:

| Target | Location |
|---|---|
| GKE | GCP Secret Manager → mounted via [Secret Manager CSI driver](https://cloud.google.com/secret-manager/docs/secret-manager-managed-csi-component) |
| EKS | AWS Secrets Manager → mounted via [AWS Secrets and Configuration Provider (ASCP)](https://github.com/aws/secrets-store-csi-driver-provider-aws) |
| AKS | Azure Key Vault → mounted via [Secrets Store CSI driver](https://learn.microsoft.com/en-us/azure/aks/csi-secrets-store-driver) |
| Bare metal | HashiCorp Vault → mounted via [Vault Agent injector](https://developer.hashicorp.com/vault/docs/platform/k8s/injector) |

The Bodhyanga deployment manifests (`zarf/k8s/prod/`) reference a `bodhyanga-care-secrets`
Secret resource — populate that via SealedSecret, ExternalSecret, or directly
from your chosen secret store. **Do not** copy `*.pem` files from this directory
into a prod-shaped Secret resource.

## Generating a new key

### RS256 (RSA)

```bash
# 2048-bit private key (the auth surface accepts 2048+; 4096 is fine).
openssl genrsa -out private.pem 2048

# Public key (export from the private file).
openssl rsa -in private.pem -pubout -out public.pem

# Rename to a UUID so the kid (key ID) claim can route to the right key on
# rotation — same convention as ardanlabs/service.
mv private.pem "$(uuidgen | tr 'A-Z' 'a-z').pem"
```

### EdDSA (Ed25519) — preferred for new keys

```bash
# Ed25519 is faster, has smaller signatures, and is not affected by the
# RSA implementation pitfalls (e.g. PKCS#1 v1.5 padding oracles).
openssl genpkey -algorithm ed25519 -out private.pem
openssl pkey -in private.pem -pubout -out public.pem
```

## Rotation

The intended flow:

1. Generate the new keypair with a fresh UUID filename.
2. Add it to the key store *alongside* the current key (do not replace).
3. Push a config change that adds the new `kid` to the verifier list while
   keeping the old `kid` valid — so tokens minted under the old key still
   verify until they expire.
4. Cut new tokens with the new `kid`.
5. After the longest token TTL elapses (typically 24 h or 7 d), remove the
   old key from the verifier list and from the key store.

## What ships in this directory

| File | Purpose |
|---|---|
| `54bb2165-71e1-41a6-af3e-7da4a0e1e2c1.pem.example` | Sample RS256 private key — DO-NOT-USE-IN-PROD. The filename mirrors the upstream Ardan Labs sample. The `.example` suffix is intentional so this file is never accidentally read by a real auth service. |

The literal sample key bytes from the Ardan Labs repo are **not** vendored
here — every Bodhyanga deployment generates its own keys per the recipe above.
The `.example` file is empty by design: copy it to `<uuid>.pem`, then
`openssl genrsa -out <uuid>.pem 2048` to materialise the actual key bytes.
