# Security scope

This starter is designed for one trusted operator workspace and one GitHub repository. Keep reviewer access separate from the proposing agent. Do not expose it as a multi-tenant service without additional identity, authorization, rate-limiting, and isolation work.

Reports and model output are untrusted content. The model only returns a draft; the server controls the destination and allowed action. Human review is still needed for unsupported claims, sensitive data, links, mentions, and content that could trigger repository automations.

Never include tokens, private reports, cloud instance identifiers, or provider payloads in public issues. Use the repository owner's private security reporting channel when one is available. If private reporting is not enabled, contact the maintainer privately instead of disclosing exploit details in a public issue.

The history is an operational review record, not a tamper-proof compliance audit. See [production boundaries and recovery](docs/PRODUCTION.md) before deployment.
