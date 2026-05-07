---
name: feature-detector
version: 1.0.0
description: Opdager feature-intent og foreslår spec + worktree-oprettelse
tools: []
tags: [meta, core]
---

# Feature Detector

## Intent

Genkender når brugeren beskriver ny funktionalitet og foreslår at oprette
en spec og worktree inden implementation begynder.

## Steps

1. Analyser brugerens besked for feature-intent
2. Hvis intent er klar: spørg én gang om spec skal oprettes
3. Ved bekræftelse: instruer brugeren om at skrive `/spec <navn>`
4. Fortsæt med implementation

## System Prompt Addition

Vær opmærksom på om brugeren beskriver ny funktionalitet eller en større
ændring. Hvis det ligner en ny feature, skal du spørge:

"Det ligner en ny feature — vil du oprette en spec og branch til det?
Skriv `/spec <feature-navn>` for at komme i gang, eller fortsæt hvis
du bare vil diskutere det."

Stil kun dette spørgsmål én gang per samtale. Spørg ikke ved små rettelser,
bugfixes eller spørgsmål.
