# Project Guidelines

## Plan Management

- All feature plans are stored in `docs/plans/`, organized by feature in their own directories.
- Each feature directory contains a `plan.md` file describing the design, requirements, and implementation steps for that feature.
- Plan directories must be prefixed with numbers in the correct implementation order (e.g., `01-project-setup`, `02-database`, `03-user-entity`). When adding a new plan, assign the next sequential number.
- When creating or updating plans:
  1. Identify the feature scope and create/update the corresponding directory under `docs/plans/<NN-feature-name>/` (where `NN` is the sequence number).
  2. Each plan should include: **Overview**, **Requirements**, **Design**, **Implementation Steps**, **Testing**, and **Acceptance Criteria**.
  3. Every plan must include a **Testing** section that describes the unit tests to be written, covering core logic, edge cases, and error/negative scenarios.
  4. Plans should be kept up to date as implementation progresses — mark completed steps and note any deviations.
- Cross-cutting concerns (e.g., database schema shared across features) get their own plan directory.
- Reference related plans from within a plan when there are dependencies between features.

## Testing

- Every implementation change must include unit tests.
- Tests should cover the core logic, edge cases, and error/negative scenarios for the changed code.
- Do not merge or consider a feature complete without accompanying unit tests.
