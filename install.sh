#!/bin/sh
# Script de instalación global para sv-memory
set -e

# Configuración del repositorio y binario
REPO="svtech/sv-memory"
BINARY="sv-memory"
INSTALL_DIR="/usr/local/bin"

# 1. Detectar Sistema Operativo
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    darwin) OS="darwin" ;;
    linux) OS="linux" ;;
    *)
        echo "❌ Sistema operativo no soportado: $OS"
        exit 1
        ;;
esac

# 2. Detectar Arquitectura
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)
        echo "❌ Arquitectura no soportada: $ARCH"
        exit 1
        ;;
esac

# 3. Obtener el archivo comprimido desde la sección de Releases
# Nota: Durante el desarrollo local, si no hay Releases públicas en GitHub, puedes compilar manualmente
# pero este script asume la descarga de binarios precompilados de producción de GitHub Releases.
TARBALL="${BINARY}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/latest/download/$TARBALL"

echo "📥 Descargando la última versión de $BINARY para $OS/$ARCH..."
TEMP_FILE=$(mktemp)

# Descargar usando curl (con redirección de redireccionamientos -L)
if ! curl -fsSL "$URL" -o "$TEMP_FILE"; then
    echo "❌ Error al descargar el binario desde $URL"
    echo "Asegúrate de que la Release pública existe en GitHub."
    rm -f "$TEMP_FILE"
    exit 1
fi

# 4. Descomprimir e instalar en la ruta global de ejecutables
echo "⚙️  Instalando en $INSTALL_DIR/$BINARY (requiere privilegios sudo)..."
if ! sudo tar -xzf "$TEMP_FILE" -C "$INSTALL_DIR" "$BINARY"; then
    echo "❌ Error al descomprimir o mover el binario a $INSTALL_DIR"
    rm -f "$TEMP_FILE"
    exit 1
fi

rm -f "$TEMP_FILE"
echo "✅ ¡$BINARY se instaló correctamente y está listo para ser usado globalmente!"
echo "Prueba ejecutando: $BINARY --help"
