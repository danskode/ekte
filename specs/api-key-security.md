# Spec: API-nøgle sikkerhed

## Status: draft

## Intent

API-nøgler må aldrig gemmes i config.yaml som default.
Brugeren guides til env-variabel og forstår hvorfor.

## Ændringer

### Onboarding
- Spørg IKKE om API-nøgle — vis i stedet hvilken env-var der skal sættes
- Vis præcis kommando: `export ANTHROPIC_API_KEY=...`
- Fortæl hvor nøglen hentes (Anthropic Console / OpenAI Platform)
- Gem kun provider/model/base_url i config.yaml

### config.yaml
- `api_key` fjernes som felt — bruges kun fra env
- Kommentar øverst: "Gem aldrig API-nøgler her — brug env-variabel"

### Advarsel ved opstart
- Hvis `api_key` alligevel er sat i config.yaml: vis advarsel i TUI
- Hvis ingen nøgle findes (env eller config): vis klar fejl før TUI starter

## Acceptkriterier

- [ ] Onboarding gemmer aldrig nøgle i fil
- [ ] Klar vejledning med korrekt env-var-navn vist til brugeren
- [ ] Advarsel i TUI hvis nøgle er i config-fil
- [ ] Graceful fejl hvis ingen nøgle er tilgængelig
