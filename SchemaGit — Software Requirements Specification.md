# SchemaGit — Software Requirements Specification (SRS)

**Version:** 0.1 (Draft) | **Date:** October 3, 2026 | **Standard:** IEEE 830 / ISO 29148-inspired

---

## 1. Introduction

### 1.1 Purpose

This document specifies the requirements for **SchemaGit**, a Git-inspired database schema version control platform. It is intended for the founding team, contributors, and future stakeholders (QA, DevOps, design).

### 1.2 Product Scope

SchemaGit lets development teams **branch, merge, compare, and safely evolve database schemas**. It provides a canonical schema model, three-way schema merge, migration generation, pre-flight safety validation, visual diffs, conflict resolution, and complete version history.

**In scope (MVP):** PostgreSQL, CLI, local Git-backed storage, schema model, diff, 3-way merge, migration generation, safety checks, CI integration. **Out of scope (MVP):** Multi-DB support, hosted SaaS, web UI, data (row-level) versioning, RBAC/SSO.

### 1.3 Definitions

| Term | Definition |
| --- | --- |
| **Schema Model (SM)** | DB-agnostic, canonical, serializable representation of a database schema |
| **Schema Commit** | Immutable object holding an SM snapshot hash, the migration, parent(s), author, and message |
| **Branch** | Named movable pointer to a Schema Commit |
| **Migration** | Ordered SQL (up/down) that transforms schema A into schema B |
| **Drift** | Difference between the live DB and the schema at the tracked commit |
| **Shadow DB** | Disposable database used to simulate migrations |
| **Pre-flight** | Safety analysis run before a migration is applied |

### 1.4 References

Git internals (object model), PostgreSQL system catalogs, competing tools (Atlas, Flyway, Liquibase, Bytebase, Skeema, Alembic, Prisma Migrate).

---

## 2. Overall Description

### 2.1 Product Perspective

SchemaGit is a standalone CLI tool (Phase 1) that sits alongside a project's Git repo and its databases. A hosted platform with web UI and API follows in later phases.

```
Developer ── CLI ──► SchemaGit Core ──► Schema Model Store (files + Git)
                          │
                          ├──► DB Adapter (PostgreSQL)  ◄──► Live DB / Shadow DB
                          └──► CI/CD Integrations
```

### 2.2 User Classes

| User | Needs |
| --- | --- |
| **Backend Developer** | Branch schema changes, merge without conflicts, generate migrations |
| **Platform/DevOps Engineer** | Safe deploys, drift detection, CI gates |
| **DBA** | Review diffs, lock/impact analysis, approve risky changes |
| **Tech Lead / QA** | History, audit trail, reproducible environments |

### 2.3 Operating Environment

Linux, macOS, Windows (WSL); Git ≥ 2.30; PostgreSQL 13+; distributed as a single binary (suggested: Go or Rust).

### 2.4 Assumptions & Dependencies

- Teams already use Git for application code.
- Users have connection privileges to introspect databases.
- PostgreSQL `information_schema` / `pg_catalog` remain the introspection source.

### 2.5 Constraints

- Dialect-specific DDL behavior (e.g., transactional DDL) must be isolated in adapters.
- Schema Model must remain DB-agnostic at its core.
- Must not require a hosted service to function.

---

## 3. Functional Requirements

Priority: **M** = Must, **S** = Should, **C** = Could.

### 3.1 Repository & Initialization

| ID | Requirement | Pri |
| --- | --- | --- |
| FR-1.1 | `schemagit init` shall create a SchemaGit repository (config + object store) | M |
| FR-1.2 | The system shall import an initial schema from a live DB or from DDL files | M |
| FR-1.3 | The system shall store its data as plain files compatible with Git transport | M |
| FR-1.4 | The system shall support a config file defining environments (dev/staging/prod) and connections | M |

### 3.2 Schema Model & Parsing

| ID | Requirement | Pri |
| --- | --- | --- |
| FR-2.1 | The system shall define a canonical Schema Model (JSON/YAML) covering tables, columns, types, defaults, nullability, PK/FK/unique/check constraints, indexes, views, sequences, enums, functions, triggers | M |
| FR-2.2 | The system shall introspect a live PostgreSQL DB into the Schema Model | M |
| FR-2.3 | The system shall parse PostgreSQL DDL into the Schema Model | S |
| FR-2.4 | Serialization shall be deterministic (stable ordering) so equal schemas yield identical bytes/hashes | M |
| FR-2.5 | Each schema object shall carry a stable identity (ID) independent of its name, enabling rename detection | M |
| FR-2.6 | The Schema Model shall be validated against a published spec (JSON Schema) | S |

### 3.3 Commits & History

| ID | Requirement | Pri |
| --- | --- | --- |
| FR-3.1 | `schemagit commit` shall create an immutable Schema Commit containing: schema hash, migration, parent(s), author, timestamp, message | M |
| FR-3.2 | Commits shall be content-addressed (hash-identified) | M |
| FR-3.3 | `schemagit log` shall show history with filters (branch, author, object, date) | M |
| FR-3.4 | `schemagit show <commit>` shall display the schema state and migration of a commit | M |
| FR-3.5 | `schemagit blame <object>` shall show which commit last changed a table/column | C |
| FR-3.6 | Tags shall mark releases (`schemagit tag v1.2`) | S |

### 3.4 Branching

| ID | Requirement | Pri |
| --- | --- | --- |
| FR-4.1 | Users shall create, list, switch, rename, and delete branches | M |
| FR-4.2 | Branch operations shall be cheap (pointer-only, no DB copying) | M |
| FR-4.3 | The system shall map branches to Git branches optionally (linked mode) | S |
| FR-4.4 | The system shall support ephemeral database provisioning per branch (shadow/preview DBs) | C |

### 3.5 Diff

| ID | Requirement | Pri |
| --- | --- | --- |
| FR-5.1 | `schemagit diff A..B` shall compute a semantic diff between two commits, branches, or a commit and a live DB | M |
| FR-5.2 | Diff output shall be classified: added / removed / modified / renamed, per object | M |
| FR-5.3 | The system shall detect probable renames (column/table) using identity and similarity heuristics, requiring user confirmation when ambiguous | M |
| FR-5.4 | Diff shall be available as text, JSON, and SQL | M |
| FR-5.5 | A visual diff (side-by-side, ER-style) shall be provided | S (post-MVP) |

### 3.6 Merge & Conflict Resolution

| ID | Requirement | Pri |
| --- | --- | --- |
| FR-6.1 | `schemagit merge <branch>` shall perform a three-way merge (base, ours, theirs) on the Schema Model | M |
| FR-6.2 | Non-overlapping changes shall merge automatically | M |
| FR-6.3 | The system shall detect semantic conflicts, including: same object modified differently; modify vs. delete; conflicting renames; type change vs. dependent index/FK/view; duplicate names; ordering/dependency cycles | M |
| FR-6.4 | Merge shall produce a conflict report listing each conflict, its cause, and suggested resolutions | M |
| FR-6.5 | Users shall resolve conflicts via ours / theirs / manual edit, per object | M |
| FR-6.6 | Post-merge, the system shall validate that the resulting schema is internally consistent (no dangling FKs, valid dependencies) | M |
| FR-6.7 | `schemagit merge --abort` shall restore pre-merge state | M |
| FR-6.8 | Interactive/visual conflict resolution UI shall be provided | S (post-MVP) |

### 3.7 Migration Generation & Execution

| ID | Requirement | Pri |
| --- | --- | --- |
| FR-7.1 | The system shall generate up migrations from schema A → B | M |
| FR-7.2 | The system shall generate down (rollback) migrations where reversible and flag irreversible steps | M |
| FR-7.3 | Migration steps shall be dependency-ordered (e.g., drop FK before dropping table) | M |
| FR-7.4 | `schemagit migrate up/down [--to <commit>]` shall apply migrations to a target DB | M |
| FR-7.5 | The system shall record applied migrations in a tracking table in the target DB | M |
| FR-7.6 | Migrations shall support a dry-run mode that prints SQL without executing | M |
| FR-7.7 | The system shall support hand-written migration overrides attached to a commit | S |
| FR-7.8 | Data-migration hooks (pre/post SQL scripts) shall be supported | S |
| FR-7.9 | Where supported, migrations shall run transactionally | S |

### 3.8 Safety Layer (Pre-flight Validation)

| ID | Requirement | Pri |
| --- | --- | --- |
| FR-8.1 | The system shall detect destructive operations (DROP TABLE/COLUMN, type narrowing, NOT NULL additions without default) and require explicit confirmation | M |
| FR-8.2 | The system shall estimate lock impact and flag operations that take heavy locks (e.g., table rewrites, non-concurrent index builds) | S |
| FR-8.3 | The system shall check potential data loss against live DB statistics | S |
| FR-8.4 | The system shall simulate migrations on a shadow DB and report success/failure | S |
| FR-8.5 | The system shall suggest safer alternatives (e.g., `CREATE INDEX CONCURRENTLY`, expand/contract pattern) | C |
| FR-8.6 | Risk level (low/medium/high) shall be assigned to each migration | S |
| FR-8.7 | Safety rules shall be configurable and overridable by policy | S |

### 3.9 Drift Detection

| ID | Requirement | Pri |
| --- | --- | --- |
| FR-9.1 | `schemagit status` shall compare a live DB against its expected commit and report drift | M |
| FR-9.2 | The system shall offer to adopt drift as a new commit or generate a migration to revert it | S |
| FR-9.3 | Scheduled drift checks shall be runnable in CI | S |

### 3.10 CI/CD Integration

| ID | Requirement | Pri |
| --- | --- | --- |
| FR-10.1 | CLI shall return deterministic exit codes and machine-readable (JSON) output | M |
| FR-10.2 | The system shall provide `schemagit check` for PR gates: validity, conflicts, destructive-change policy | M |
| FR-10.3 | First-party GitHub Actions / GitLab CI templates shall be provided | S |
| FR-10.4 | PR comments summarizing schema diffs shall be supported | C |

### 3.11 Interoperability

| ID | Requirement | Pri |
| --- | --- | --- |
| FR-11.1 | The system shall export migrations in formats compatible with Flyway, Liquibase, and Alembic | C |
| FR-11.2 | The system shall import existing migration folders to bootstrap history | C |

### 3.12 Later Phases (Hosted Platform)

Web UI with visual schema diffs and ER diagrams; REST/GraphQL API; user/team management with RBAC; pull-request-style schema reviews and approvals; audit logs; webhooks; additional dialects (MySQL, SQLite, SQL Server).

---

## 4. Non-Functional Requirements

| ID | Category | Requirement |
| --- | --- | --- |
| NFR-1 | **Performance** | Introspect and diff a 1,000-table schema in under 10 s; 3-way merge in under 5 s |
| NFR-2 | **Scalability** | Support schemas up to 10,000 objects and histories of 100,000 commits |
| NFR-3 | **Reliability** | All state-changing operations (merge, migrate) shall be atomic or safely resumable; no partial corruption of the object store |
| NFR-4 | **Correctness** | Applying a generated migration to schema A must yield schema B exactly (verified by round-trip tests) |
| NFR-5 | **Determinism** | Same inputs always produce the same hashes, diffs, and migrations |
| NFR-6 | **Security** | Credentials never stored in plaintext in the repo; support env vars and secret managers; TLS for DB connections; read-only introspection role supported |
| NFR-7 | **Privacy** | MVP shall not read or transmit row data; telemetry opt-in only |
| NFR-8 | **Usability** | Git-like command naming; actionable error messages; `--help` on every command; install in under 2 minutes |
| NFR-9 | **Portability** | Single static binary for Linux/macOS/Windows |
| NFR-10 | **Extensibility** | Adapter interface so new dialects are added without changing core |
| NFR-11 | **Maintainability** | ≥ 80% unit-test coverage on core merge/diff logic; property-based tests for merge |
| NFR-12 | **Observability** | Structured logs, verbose/debug modes |
| NFR-13 | **Compatibility** | PostgreSQL 13–17 supported; documented behavior across versions |

---

## 5. External Interface Requirements

### 5.1 User Interface (CLI)

```
schemagit init | status | commit | log | show | diff
schemagit branch | switch | merge | tag
schemagit migrate up | down | plan
schemagit check | drift | import | export
```

### 5.2 Software Interfaces

- **Database:** PostgreSQL wire protocol (via driver) for introspection and migration.
- **Git:** invoked through CLI or library for transport and linked branches.
- **CI:** exit codes, JSON output, container image.

### 5.3 File Formats

- Schema Model: YAML/JSON, one file per object (table/view/etc.) to minimize Git conflicts.
- Config: `schemagit.yaml`.

---

## 6. Data Requirements

### 6.1 Core Entities

| Entity | Key Fields |
| --- | --- |
| **Repository** | id, config, default branch |
| **SchemaCommit** | hash, parents\[\], schema_hash, migration_id, author, timestamp, message |
| **SchemaSnapshot** | hash, objects\[\] |
| **SchemaObject** | object_id (stable), type, name, definition, dependencies\[\] |
| **Migration** | id, up_sql, down_sql, reversible, risk_level, from_hash, to_hash |
| **Branch / Tag** | name, target_commit |
| **Environment** | name, connection ref, current_commit |
| **AppliedMigration** (in target DB) | migration_id, checksum, applied_at, applied_by |

### 6.2 Storage

Object store with content-addressed objects; human-readable files in the working tree; Git as optional sync/transport layer.

---

## 7. Merge Semantics (Key Design Rules)

1. Merge operates on the **Schema Model**, never on raw SQL text.
2. Objects are matched by stable ID first, then by name/similarity for rename detection.
3. A change is *conflicting* only when both sides modify the same attribute of the same object differently, or when a modification invalidates a dependency.
4. Dependency graph (tables → FKs → indexes → views) must be re-validated after merge.
5. Migrations are **re-generated** from the merged schema, not concatenated from branch migrations.

---

## 8. Release Plan

| Phase | Scope |
| --- | --- |
| **Phase 0 – Validation** | 10 developer interviews, competitor audit, Schema Model + commit spec |
| **Phase 1 – MVP** | Postgres, Schema Model, introspection, commit/log/branch, diff, 3-way merge, migration generation, dry-run, destructive-change detection, CI `check` |
| **Phase 2 – Safety** | Lock analysis, shadow DB simulation, drift detection, risk scoring, policies |
| **Phase 3 – Collaboration** | Hosted service, web UI, visual diff and conflict resolution, PR-style reviews, RBAC |
| **Phase 4 – Expansion** | MySQL/SQLite/SQL Server, import/export for Flyway/Liquibase, plugins |

---

## 9. Acceptance Criteria (MVP)

- AC-1: A user can import an existing Postgres DB, commit it, branch, change the schema, and merge back.
- AC-2: Two branches adding different tables/columns merge with zero manual steps.
- AC-3: Conflicting edits (same column altered differently) are detected and presented with resolution options.
- AC-4: Generated migration applied to a clean DB reproduces the target schema exactly (round-trip test passes).
- AC-5: Destructive migrations are blocked without explicit confirmation.
- AC-6: `schemagit check` runs in CI and fails the build on conflicts or policy violations.

---

## 10. Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Merge semantics are harder than text merges | High | Prototype 3-way merge first; property-based tests |
| Dialect sprawl | High | Postgres-only MVP; strict adapter boundary |
| Live DB drift | High | Drift detection as a first-class feature |
| Irreversible migrations | Medium | Flag explicitly; require confirmation/backups |
| Adoption vs. Flyway/Atlas | High | Differentiate on branching, merge, and safety; import existing history |

---

## 11. Open Questions

1. Implementation language (Go vs. Rust)?
2. Is Git the source of truth, or a transport for SchemaGit's own object store?
3. Open-source core + paid hosted tier?
4. How are rename ambiguities resolved in non-interactive CI mode?
5. How are data migrations (backfills) modeled alongside schema migrations?

---

*End of Document — v0.1*