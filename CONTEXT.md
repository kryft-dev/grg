# grg Domain Model

`grg` is a standalone command-line search tool that provides ripgrep-like search semantics directly across Git repository history without checking out commits.

## Language

**Blob OID**:
The cryptographic object identifier (SHA-1 or SHA-256) of an immutable Git blob object.
_Avoid_: Hash, file hash, blob id

**Provenance**:
The historical lineage metadata associated with a blob occurrence, including the commit SHA, author date, committer date, and tree path.
_Avoid_: Attribution, blame info, history trace

**Introducing Commit**:
The commit where a specific blob version was introduced or modified for a given file path.
_Avoid_: First commit, origin commit, author commit

**Delta Forest**:
A directed acyclic structure representing Git packfile base objects and their chain of `OBJ_OFS_DELTA` / `OBJ_REF_DELTA` children.
_Avoid_: Delta tree, pack graph

**Tree Entry**:
A directory record in a Git tree associating a path name and mode with a blob or subtree OID.
_Avoid_: Directory node, file pointer
