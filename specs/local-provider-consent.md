# Spec: Local Provider Consent

## Status: implemented

## Intent

Fjern behovet for `EKTE_ALLOW_LOCAL_PROVIDER`-miljøvariablen ved almindelig
brug af lokale providers (Ollama, LM Studio). I stedet gives samtykke én gang
interaktivt og gemmes persistent — pr. præcis URL — i brugerens **globale**
ekte-mappe.

## Design

- Ny pakke `internal/consent` med `consent.yaml` i `~/.ekte/` (0600).
  Filen ejes af brugeren og ligger ALDRIG i projektets `.ekte/` — en klonet
  eller manipuleret projekt-config kan derfor ikke give sig selv samtykke.
- Samtykke matches på den **præcise** URL-streng (trimmet). Ændres
  `base_url` — også bare port eller sti — kræves nyt samtykke.
- Samtykke kan kun opstå tre steder, alle interaktive:
  1. Opstartsdialog i terminalen ("Tillad? [j/n]") når config peger på en
     privat adresse uden gemt samtykke — i stil med onboardingens tillidstrin.
  2. Ollama-opsætningen i API-guiden (brugeren har selv indtastet URL'en).
  3. Model-wizardens/`/model`-kommandoens bekræftelsestrin ('j') — agenten
     kalder `GrantLocalURL`-callbacken, som kun main.go kan wire op.
- `OnProviderReload` (kaldes efter wizard-gem) genvaliderer disk-config'ens
  URL mod samtykkelisten — en ekstern ændring af config-filen mellem
  bekræftelse og reload afvises.
- `EKTE_ALLOW_LOCAL_PROVIDER=1` bevares som global override til
  headless/scriptet brug og springer både dialog og samtykketjek over.
- Runtime-håndhævelsen (DNS-rebinding-tjek i `openai.go`'s DialContext)
  bevares uændret og aktiveres nu af `Config.AllowLocal` ELLER env-varen.

## Acceptkriterier

- [x] Opstart med privat `base_url` uden samtykke viser j/n-dialog
- [x] "j" gemmer samtykke i `~/.ekte/consent.yaml` (0600) med præcis URL
- [x] "n" afslutter uden at gemme noget
- [x] Gemt samtykke → ingen dialog ved næste opstart
- [x] Ændret `base_url` → dialog igen
- [x] `EKTE_ALLOW_LOCAL_PROVIDER=1` springer dialogen over (headless)
- [x] Projekt-config kan ikke selv give samtykke (filen læses kun fra global mappe)
- [x] Tests for valideringslogik med statiske fixtures — ingen netværkskald
