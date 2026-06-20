#!/bin/sh
set -e

REPO="danskode/ekte"

# Vælg installationsmappe. Foretræk /usr/local/bin når den er skrivbar (typisk
# som root / i en container): den er allerede i PATH, så 'ekte' virker straks —
# uden at redigere PATH eller genindlæse shell. Ellers ~/.local/bin (ingen sudo),
# som så håndteres med PATH-tilføjelse nedenfor.
if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
  BIN_DIR="/usr/local/bin"
else
  BIN_DIR="${HOME}/.local/bin"
fi

# Detektér OS
OS="$(uname -s)"
case "${OS}" in
  Linux)  OS="linux" ;;
  Darwin) OS="darwin" ;;
  *)
    echo "Fejl: ekte understøtter ikke ${OS} endnu." >&2
    exit 1
    ;;
esac

# Detektér arkitektur
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64)        ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Fejl: ekte understøtter ikke ${ARCH} endnu." >&2
    exit 1
    ;;
esac

# Kræv curl eller wget
if command -v curl >/dev/null 2>&1; then
  _get() { curl -fsSL "$1"; }
  _download() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  _get() { wget -qO- "$1"; }
  _download() { wget -q "$1" -O "$2"; }
else
  echo "Fejl: curl eller wget er påkrævet." >&2
  exit 1
fi

echo "Henter seneste version af ekte..."

# Hent seneste release-tag fra GitHub API
LATEST_TAG=$(_get "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' \
  | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

if [ -z "${LATEST_TAG}" ]; then
  echo "Fejl: Kunne ikke hente seneste release. Prøv igen om lidt." >&2
  exit 1
fi

echo "Version: ${LATEST_TAG}"

ARCHIVE="ekte_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}"

# Download til midlertidig mappe
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

echo "Downloader ${ARCHIVE}..."
_download "${BASE_URL}/${ARCHIVE}"       "${TMP}/${ARCHIVE}"
_download "${BASE_URL}/checksums.txt"    "${TMP}/checksums.txt"

# Verificér checksum
echo "Verificerer checksum..."
cd "${TMP}"
if command -v sha256sum >/dev/null 2>&1; then
  grep "${ARCHIVE}" checksums.txt | sha256sum --check --status || {
    echo "Fejl: Checksum-mismatch — download kan være beskadiget." >&2
    exit 1
  }
elif command -v shasum >/dev/null 2>&1; then
  grep "${ARCHIVE}" checksums.txt | shasum -a 256 --check --status || {
    echo "Fejl: Checksum-mismatch — download kan være beskadiget." >&2
    exit 1
  }
else
  echo "Advarsel: sha256sum/shasum ikke fundet — springer checksum over."
fi

# Installér
echo "Installerer..."
tar -xzf "${ARCHIVE}"
mkdir -p "${BIN_DIR}"
mv ekte "${BIN_DIR}/ekte"
chmod +x "${BIN_DIR}/ekte"

echo ""
echo "✓ ekte ${LATEST_TAG} installeret"
echo ""

# Tjek om BIN_DIR er i PATH
case ":${PATH}:" in
  *":${BIN_DIR}:"*)
    echo "Klar til brug:"
    echo ""
    echo "  cd dit-projekt"
    echo "  ekte"
    ;;
  *)
    # ~/.local/bin er ikke i PATH. Tilføj det til brugerens shell-rc, så 'ekte'
    # virker i nye shells — idempotent (kun hvis linjen ikke allerede findes).
    LINE='export PATH="$HOME/.local/bin:$PATH"'
    case "${SHELL:-}" in
      *zsh) RC="${HOME}/.zshrc" ;;
      *)    RC="${HOME}/.bashrc" ;;
    esac
    UPDATED=""
    for f in "${RC}" "${HOME}/.profile"; do
      if [ -f "${f}" ] && grep -qF "${LINE}" "${f}" 2>/dev/null; then
        continue   # allerede tilføjet
      fi
      printf '\n# ekte\n%s\n' "${LINE}" >> "${f}" 2>/dev/null && UPDATED="${UPDATED} ${f}"
    done

    if [ -n "${UPDATED}" ]; then
      echo "✓ Tilføjede ~/.local/bin til din PATH i:${UPDATED}"
    else
      echo "${BIN_DIR} er ikke i din PATH. Tilføj manuelt:"
      echo "  echo '${LINE}' >> ~/.bashrc"
    fi
    echo ""
    echo "Brug ekte i DENNE terminal nu:"
    echo ""
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo "  cd dit-projekt && ekte"
    echo ""
    echo "(I nye terminaler virker 'ekte' automatisk.)"
    ;;
esac
echo ""
