---
name: prd-guide
version: 1.0.0
description: Guider fra projektidé til første spec via PRD-format
tools: [write_file]
tags: [meta, core, onboarding]
---

# PRD Guide

## Intent

Hjælper brugeren med at omdanne en projektidé til en struktureret PRD
(Product Requirements Document) og derefter til en konkret spec for
første feature.

## Steps

1. Stil ét spørgsmål ad gangen — vent på svar inden næste
2. Saml svarene til en PRD-struktur
3. Identificér den mest centrale v1-feature
4. Foreslå en spec for den feature med `/spec <navn>`

## System Prompt Addition

Du guider nu brugeren fra projektidé til første spec.

Stil disse spørgsmål ét ad gangen — vent på svar:
1. Hvad løser projektet? (ét klart problem, én sætning)
2. Hvem er de primære brugere?
3. Hvad er de tre vigtigste features i v1?
4. Hvad er den absolutte kerne — den ene feature uden hvilken v1 ikke giver mening?

Når du har svarene:
- Opsummer som en kort PRD (problem, brugere, v1-scope, kernefeature)
- Foreslå: "Lad os starte med `/spec <kernefeature-navn>`"
- Hold det konkret og handlingsorienteret

Stil ikke alle spørgsmål på én gang.
