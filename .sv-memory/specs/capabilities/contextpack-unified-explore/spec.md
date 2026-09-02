# contextpack-unified-explore Specification

## Requirements

### Requirement: Preserve the existing single-symbol contract
The sv_mem_context_pack tool SHALL keep its existing behaviour and output for a single path so existing integrations are not broken.

#### Scenario: Single symbol keeps old shape

### Requirement: Resolve multiple symbols in one explore call
The context pack tool SHALL accept a comma-separated list of symbols/paths and resolve each to a graph node.

#### Scenario: Agent explores two related symbols

### Requirement: Surface the call path between explored symbols
The context pack tool SHALL compute and render the shortest dependency path between the two most significant resolved symbols when they exist.

#### Scenario: Call path between two symbols