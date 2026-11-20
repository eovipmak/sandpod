# Modules

Place independently deployable components in this directory. A module should
own its:

- build and package definitions;
- service/Quadlet units;
- configuration templates;
- tests or verification commands; and
- operator documentation, including dependencies and rollback notes.

Modules should communicate through explicit interfaces (ports, files,
credentials, and documented commands) rather than reaching into another
module's private files. Put cross-module decisions in `docs/`.

Planned modules are listed in [the architecture document](../docs/architecture.md).
