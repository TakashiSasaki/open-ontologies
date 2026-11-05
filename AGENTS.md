# Native fork development

- Read `FORK.md` for the upstream base, plan and current verification scope.
- Keep generic protocol/proxy work in the separate adapter projects. Do not
  publish host credentials, actual requests, registries or deployment data.
- `patches/proposed-native/` contains proposals for an older exact base. They
  are not automatically applied or freshly qualified engine source. Preserve
  their provenance and describe any changes made while rebasing.
- Keep independent fixes in separate commits with focused reproductions and
  relevant native tests. Distinguish apply checks from compiled/runtime tests.
- Preserve upstream versions; identify modified builds with
  `+local.<short-sha>[.dirty]`. A source patch archive is not a release.
- Preserve upstream MIT notices. For this user's contributions, include:
  `Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)`.
- Public fork work is authorized; production deployment is a separate operation.
