---
name: intent-spec-guide
version: 1.0.0
description: Guider brugeren igennem formulering af Intent-Specs med IDD's sproglige bevidsthed om kontekstformning og misforståelsesforebyggelse
tools: [write_file, read_file]
tags: [meta, core, spec, idd]
---

# Intent-Spec Guide

## Intent

Hjælper brugeren med at formulere komplette, validerede Intent-Specs for nye
features og projekter. Inkorporerer IDD's (Intent-Driven Development) sproglige
principper: forståelse er en konstruktionsproces, ikke datatransfer. Tvinger
brugeren til at gøre det underforståede eksplicit, undgår vag kommunikation,
og sikrer at konteksten formes bevidst — så agenten skaber den rigtige
forståelse og ikke udfylder huller arbitrært.

## IDD's Sproglige Principper

Dette er fundamentet for hele guiden:

1. **Forståelse er konstruktion** — Sprog overfører ikke færdig forståelse som
   en pakke. Forståelse *skabes* når modtageren (agenten) læser din tekst i sin
   kontekst. Du *bygger* agentens forståelse gennem dine ord.

2. **Det Underforståede er Farligt** — Alt hvad du ikke siger, udfylder agenten
   med sine antagelser. Det er her `misuse` (forkert anvendelse), `misfire`
   (fejlslået eksekvering) og `hallucination` opstår.

3. **Kontekst er Kontrakt** — Den kontekst du præsenterer for agenten *er*
   den kontrakt den arbejder inden for. Ufuldstændig kontekst = ufuldstændig
   kontrakt = utilsigtede resultater.

4. **Vaghed er Teknisk Gæld** — "Gør det smart", "håndter fejl ordentligt",
   "gør det brugervenligt" er ikke instruktioner — det er håb. Agenten kan
   ikke læse dine tanker.

5. **Eksplicitet er Sikkerhed** — Jo tydeligere du definerer grænser,
   forudsætninger og forventninger, jo mindre rum har agenten til at
   improvisere forkert.

## Steps

Følg disse trin i rækkefølge. Stil ét spørgsmål ad gangen — vent på svar.

### Trin 0: Forståelsescheck

Før vi starter: forklar hvorfor denne spec findes.

> "Vi laver denne spec fordi agenten ikke kan gætte hvad du mener.
> Alt hvad du ikke skriver, vil agenten udfylde med sine egne antagelser.
> Derfor gennemgår vi systematisk: hvad du vil have, hvad du *ikke* vil have,
> og hvilken kontekst agenten skal forstå opgaven i."

### Trin 1: Feature-Navn og Formål

Spørg:
1. Hvad hedder featuret/projektet? (kort, kebab-case)
2. Hvad er det *én sætning* formål?

**IDD-check:** Er formålet handlingsorienteret eller resultatorienteret?
- Dårligt: "Vi skal have en wiki" (vagt, ingen handling)
- Godt: "Brugeren kan gemme og søge projektviden via `/wiki` kommandoen"
  (konkret handling, konkret resultat)

### Trin 2: Description — Hvad og Hvorfor

Guid brugeren til at skrive Description-sektionen:

> "Beskriv featuret i 2-4 sætninger. Dæk:
> - **Hvad** det gør (konkret funktionalitet)
> - **Hvorfor** det findes (problemet det løser)
> - **Hvem** der bruger det (rolle/persona)
>
> **IDD-advarsel:** Hvis du skriver 'det skal være intuitivt' eller 'det skal
> håndtere alle edge cases', har du ikke sagt noget. Erstat med konkrete
> eksempler: 'intuitivt = brugeren finder funktionen uden dokumentation'
> eller 'edge cases = tom input, specialtegn, meget lang tekst (>1000 tegn)'."

### Trin 3: Constraints — Grænserne der Forhindre Misuse

Dette er den vigtigste sektion for at undgå misfire. Constraints er ikke
"nice-to-haves" — de er sikkerhedsnet.

Guid brugeren gennem disse kategorier (spørg én ad gangen):

**A. Platform/Tekniske Constraints**
> "Hvilke teknologier/sprog/frameworks skal bruges? Hvad må *ikke* bruges?
> Eksisterer der integrationer der skal respekteres?"

**B. Arkitektoniske Constraints**
> "Hvordan skal featuret tilpasses eksisterende arkitektur? Hvilke pakker/
> lag skal det bo i? Skal det følge eksisterende patterns?"

**C. Sikkerheds- og Tilladelses-Constraints**
> "Hvilke handlinger kræver bruger-tilladelse? Hvad må featuret *aldrig*
> gøre uden eksplicit godkendelse? (Fx: slette filer, kalde eksterne APIs,
> ændre config)"

**D. Performance-Constraints**
> "Hvad er acceptable responstider? Må det køre synkront eller skal det
> være async? Er der memory/CPU-begrænsninger?"

**E. Model/Provider-Constraints**
> "Hvilke LLM-providers skal understøttes? Er der features der kun
> virker med visse modeller?"

**IDD-check efter hver constraint:**
> "Hvad er den værste ting der kan ske hvis denne constraint ikke
> overholdes? Hvis svaret er 'nok intet', så er det måske ikke en
> constraint — det er en præference."

### Trin 4: Failure Scenarios — Hvad Kan Gå Gal

Her gør vi det underforståede eksplicit. De fleste bugs starter som
uovervejede failure scenarios.

Guid brugeren gennem disse kategorier:

**A. Input-Fejl**
> "Hvad sker der hvis brugeren sender: tom input, meget lang input,
> specialtegn, sarkasme, modsætningsfulde instruktioner?"

**B. Ekstern Afhængighed Fejl**
> "Hvad sker der hvis: API er nede, timeout, rate limit, ugyldig nøgle,
> filsystemet er skrivebeskyttet?"

**C. Tilstand-Fejl**
> "Hvad sker der hvis: featuret kaldes midt i en anden operation,
> tilstanden er inkonsistent, data mangler?"

**D. Sikkerheds-Scenarier**
> "Kan brugeren misbruge featuret? Kan det eksponere sensitive data?
> Kan det udføre uautoriserede handlinger?"

**E. Kontekst-Overload**
> "Hvad sker der hvis konteksten bliver for stor? Skal der komprimeres?
> Hvornår skal historik kasseres?"

**IDD-princip:**
> "Hvert failure scenario skal have et *defineret* resultat. 'Det skal
> håndtere det' er ikke nok. Skriv: 'Det skal returnere fejlmeddelelse X
> og ikke ændre nogen tilstand'."

### Trin 5: Success Scenarios — Hvad "Godt" Ser Ud

Definér konkret hvad succes betyder — ikke abstrakt "det virker".

> "Beskriv 3-5 konkrete scenarier hvor featuret fungerer perfekt:
> - Onboarding: første gang brugeren møder featuret
> - Hverdagsbrug: den mest almindelige flow
> - Avanceret brug: power-user scenarier
> - Gendannelse: hvordan brugeret recoverer efter en fejl
>
> **IDD-check:** Kan du *se* succes-scenariet? Hvis du ikke kan
> visualisere den præcise interaktion, er scenariet for vagt."

### Trin 6: Connections — Systemkomponenter og Ansvar

Kortlæg hvilke systemdele der er involveret og deres ansvar.

> "List hvilke komponenter/pakker der er involveret:
> - Hvilken pakke indeholder kerne-logikken?
> - Hvilken pakke håndterer præsentation?
> - Hvilke eksisterende services interfaces skal der implementeres?
> - Hvilke datastrukturer skal oprettes/ændres?
>
> **IDD-princip:** Tydelige grænser mellem komponenter forhindrer
> 'bleed-through' hvor én komponent gør noget der burde være en
> anden komponents ansvar."

### Trin 7: Kontekstformning — Den Mest Oversete Del

Dette er IDD's kerne: agenten skaber forståelse ud fra den kontekst
du præsenterer. Du skal *forme* denne kontekst bevidst.

Guid brugeren gennem disse spørgsmål:

**A. Referencer**
> "Hvilke eksisterende filer/specs/kode skal agenten kende for at
> forstå opgaven? List dem eksplicit."

**B. Terminologi**
> "Er der domænespecifikke begreber der skal defineres? Hvad betyder
> 'worktree', 'skill', 'provider' i *dette* projects kontekst?"

**C. Stil og Konventioner**
> "Hvilke kodningskonventioner skal følges? Hvordan ser god kode ud
> i dette projekt? (Fx: error handling pattern, naming conventions)"

**D. Eksempel-Forankring**
> "Kan du give et konkret eksempel på eksisterende kode der ligner
> hvad der skal bygges? Dette forankrer agentens forståelse."

**E. Anti-Eksempler**
> "Kan du vise et eksempel på hvad det *ikke* skal gøre? Anti-eksempler
> er ofte mere effektive end positive beskrivelser."

**IDD-advarsel:**
> "Agenten har ingen 'fælles forståelse' af dit projekt udover hvad
> du skriver i denne spec og de filer du refererer til. Hvis du ikke
> nævner at 'vi bruger always bubbletea til TUI', vil agenten måske
> vælge et andet framework. Kontekstformning er ikke optional — det
> er kontrakt-udformning."

### Trin 8: Spec-Generering og Validering

Når alle trin er gennemført:

1. Generér den komplette Intent-Spec i YAML/MD-format
2. Kør validerings-checklisten
3. Foreslå at gemme til `specs/<navn>.md`

## Intent-Spec Format

```markdown
# Spec: <feature-navn>

## Status: draft | review | approved | implemented

## Intent

<Én sætning: hvad er dette specs formål?>

## Description

<2-4 sætninger: hvad, hvorfor, hvem>

## Constraints

### Platform/Tekniske
- ...

### Arkitektoniske
- ...

### Sikkerhed/Tilladelser
- ...

### Performance
- ...

### Model/Provider
- ...

## Failure Scenarios

### Input-Fejl
- **Scenario:** ...
  - **Resultat:** ...

### Ekstern Afhængighed
- **Scenario:** ...
  - **Resultat:** ...

### Tilstand-Fejl
- **Scenario:** ...
  - **Resultat:** ...

### Sikkerhed
- **Scenario:** ...
  - **Resultat:** ...

### Kontekst-Overload
- **Scenario:** ...
  - **Resultat:** ...

## Success Scenarios

1. **<Navn>**: <Beskrivelse af flow>
2. **<Navn>**: <Beskrivelse af flow>
3. **<Navn>**: <Beskrivelse af flow>

## Connections

| Komponent | Ansvar | Fil/Pakke |
|-----------|--------|-----------|
| ...       | ...    | ...       |

## Kontekst

### Referencer
- `sti/til/fil.go` — <hvorfor denne fil er relevant>
- `specs/eksisterende-spec.md` — <hvorfor>

### Terminologi
- **<Begreb>**: <Definition i projektets kontekst>

### Konventioner
- <Kodningsstil, patterns, error handling>

### Eksempler
<Relevant kode-eksempel fra eksisterende kodebase>

### Anti-Eksempler
<Hvad det ikke skal gøre>

## Acceptkriterier

- [ ] <Konkret, testbar kriterium>
- [ ] <Konkret, testbar kriterium>
```

## Validerings-Checkliste

Før specen godkendes, kør denne checkliste:

**Sproglig Tydelighed (IDD-kernen)**
- [ ] Ingen vagt sprog ("smart", "godt", "ordentligt", "intuitivt")
- [ ] Hvert statement er testbar/falsificerbar
- [ ] Det underforståede er gjort eksplicit
- [ ] Alle "skal" har en konkret betydning

**Kontekstfuldstændighed**
- [ ] Alle relevante filer er refereret
- [ ] Terminologi er defineret
- [ ] Konventioner er specificeret
- [ ] Anti-eksempler er inkluderet

**Risikodækning**
- [ ] Mindst 3 failure scenarios pr. kategori
- [ ] Hvert failure scenario har defineret resultat
- [ ] Sikkerheds-scenarier er dækket
- [ ] Kontekst-overload er overvejet

**Teknisk Specificitet**
- [ ] Constraints er konkrete (ikke "skal være hurtig" men "< 200ms")
- [ ] Connections er kortlagt med fil/pakke-navne
- [ ] Acceptkriterier er testbare
- [ ] Ingen afhængighed af agentens "fælles fornuft"

## System Prompt Addition

Du guider nu brugeren igennem formulering af en Intent-Spec for et nyt
feature eller projekt. Følg disse principper strengt:

### IDD-Sproglige Principper (GÆLDENDE FOR AL SAMTALE)

1. **Forståelse er konstruktion** — Påmind brugeren gentagne gange at
   agenten ikke kan gætte. Hvert ord de skriver bygger agentens forståelse.
   Udeladt information = agentens antagelse = potentiel fejl.

2. **Fang vagt sprog** — Når brugeren skriver "det skal være smart",
   "håndter fejl ordentligt", "gør det brugervenligt", så stop og spørg:
   "Hvad betyder det konkret? Kan du give et eksempel?"

3. **Gør det underforståede eksplicit** — Hvis brugeren siger "det skal
   virke med de eksisterende providers", så spørg: "Hvilke providers?
   Hvad skal der virke? Hvad er succeskriteriet?"

4. **Kontekstformning** — Minst én gang i guiden, påmind brugeren:
   "Husk: agenten kender kun hvad du fortæller den. Hvis du ikke nævner
   at I bruger Go modules, vil den måske foreslå Maven. Kontekst er
   kontrakt."

5. **Misuse/Misfire forebyggelse** — Ved hver constraint, spørg:
   "Hvad er den værste ting der kan ske uden denne constraint?"
   Dette tvinger brugeren til at tænke på konsekvenser.

### Interaktionsregler

- Stil **ét spørgsmål ad gangen** — vent på svar
- Brug **konkrete eksempler** når du forklarer
- **Opsamling** efter hver sektion: "Her er hvad vi har indtil nu..."
- **Valider** før du går videre: "Er denne constraint konkret nok?"
- **Advar** om huller: "Du har ikke specificeret hvad der sker ved X"

### Output

Når guiden er færdig:
1. Generér komplet Intent-Spec i formatet ovenfor
2. Kør validerings-checklisten og vis resultater
3. Foreslå at gemme til `specs/<navn>.md`
4. Spørg om brugeren vil køre `/spec <navn>` for at oprette worktree
