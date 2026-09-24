# Signed HUMAN cards in the Homeops fork

Once persisted notes contain a syntactically signed HUMAN answer, ordinary issue
updates cannot change those notes. The initial answer write and byte-identical
notes updates remain allowed. Recognition uses the deployed envelope grammar
and accepts the recorded host independently of the current writer's hostname.
This guard does not authenticate signatures or access HMAC keys.

Historical notes remain readable, including cards with multiple historical specs.
Add annotations with `bd comments add`. For a new question, create a linked
successor card with the predecessor ID and answer receipt; do not append a new
specification to signed notes. Description, design, and ordinary metadata remain
editable.

A signed closed card cannot transition to any non-closed status. Existing
`answer-consumed:<8hex>.<16hex>` and `escalated:<8hex>.<16hex>` labels cannot be
removed or replaced. Other labels remain editable. Signed cards cannot be renamed
because signatures bind the original ID.

The guard covers ordinary storage, transaction, and proxy mutation paths. Direct
administrative SQL, database import/upsert, and replication are outside this
contract. Deployment and fresh-process runtime validation are separate from
source tests.
