# Política de Seguridad

> **Idioma:** [English](SECURITY.md) | **Español**

Nos tomamos muy en serio la seguridad de **SV-Memory**. Dado que esta herramienta maneja activamente el contexto del espacio de trabajo del desarrollador, los registros de memoria y los grafos de código estructurales, salvaguardar los datos sensibles es nuestra máxima prioridad.

---

## 1. Versiones Soportadas

Soportamos y parcheamos activamente los problemas de seguridad de las siguientes versiones:

| Versión  | Soportada         |
| ------- | ----------------- |
| 0.7.x   | :white_check_mark: |
| < 0.7.0 | :x:               |

> Política: **solo el minor más reciente.** Los arreglos de seguridad llegan al minor actual
> (p. ej., 0.7.x). Se espera que los minors anteriores actualicen.

---

## 2. Reportar una Vulnerabilidad

**Por favor, no abras un issue público en GitHub para vulnerabilidades de seguridad.**

Si descubres una vulnerabilidad de seguridad (como una falla en el motor de sanitización/redacción de secretos o un posible path traversal en las consultas del grafo de código), repórtala de forma privada:

1.  **Redacta un Security Advisory:** Si el repositorio está alojado en GitHub, ve a la pestaña **Security** del repositorio y selecciona **Advisories -> New draft advisory**. Esto nos permite discutir y parchear el problema en privado.
2.  **Correo electrónico:** Alternativamente, puedes contactar directamente al equipo de SVTech o a los mantenedores del repositorio por correo en `security@svtech.software`.

Confirmaremos tu reporte en un plazo de **48 horas** y proporcionaremos un cronograma detallado para el parche.

---

## 3. Aviso Importante sobre la Redacción de Secretos

SV-Memory incluye un motor de sanitización activo (ubicado en [security.go](internal/security/security.go)) que usa patrones regex para redactar credenciales sensibles (como claves API, JWTs, claves privadas y cadenas de conexión a BD) a `[REDACTED_SECRET]` antes de guardarlas.

*   Si bien este motor está diseñado para capturar credenciales estándar, **no sustituye las buenas prácticas de seguridad**.
*   Evita guardar credenciales crudas en diarios de progreso, registros de errores o archivos de código fuente siempre que sea posible.
