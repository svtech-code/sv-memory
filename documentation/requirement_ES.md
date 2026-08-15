# Requerimientos

> **Idioma:** [English](requirement.md) | **Español**

El sistema debe ser una memoria local del proyecto, el cual registra decisiones, ideas, metodologías, contexto, estructura y todo lo necesario para seguir el hilo del proyecto. Sin embargo, también debe manejar cómo todo está conectado mediante grafos de conocimiento tal como lo hace graphify.

La idea:

- Se inicializa una vez por proyecto
- Actualiza o crea el AGENTS.md con las indicaciones para el agente utilizado
- De manera autónoma debe almacenar y registrar decisiones, contexto, metodología y todo lo relacionado.
- También debe poder generar y actualizar el grafo de conocimientos y las relaciones entre todas las funcionalidades del proyecto

El sistema debe soportar un ciclo de decisiones guiado por especificaciones (spec-driven): antes de escribir código, el agente propone un cambio, lo valida contra reglas e invariantes, y lo promueve a una memoria durable. Las propuestas pueden llevar delta requirements estructurados estilo OpenSpec (ADDED/MODIFIED/REMOVED/RENAMED, keywords RFC 2119 SHALL/MUST/SHOULD y escenarios GIVEN/WHEN/THEN) que se fusionan en un estado persistente por capability, se proyectan en un espejo Markdown legible bajo `.sv-memory/specs/`, y se conectan al grafo de conocimiento (nodos de capability con aristas `implements`) para que el contexto recuperable por el agente incluya el contrato aplicable de cada ruta.
