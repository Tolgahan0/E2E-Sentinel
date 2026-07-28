export function StubPage({
  title,
  phase,
  description,
}: {
  title: string;
  phase: string;
  description: string;
}) {
  return (
    <section className="sentinel-card" aria-labelledby="stub-heading">
      <h2 id="stub-heading">{title}</h2>
      <p>{description}</p>
      <p className="sentinel-status-unknown">Coming in {phase}. Not implemented in Phase 0.</p>
    </section>
  );
}
