# Keeping the cc-filter fork synchronized

Remotes are configured as:

```text
origin    https://github.com/anandnomura/cc-filter.git
upstream  https://github.com/wissem/cc-filter.git
```

Keep `main` identical to upstream and maintain BAP work on `bap-edge`:

```powershell
git fetch upstream --prune
git switch main
git merge --ff-only upstream/main
git push origin main

git switch bap-edge
git merge main
.\Test-Bap.ps1 -Runtime Docker
git push origin bap-edge
```

Do not merge `bap-edge` into `main`. This preserves GitHub's simple fork sync and
keeps custom changes isolated. Most BAP code is additive, and BAP Service is in
its own top-level folder, reducing merge conflicts.
