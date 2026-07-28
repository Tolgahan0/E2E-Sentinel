export default function EnvironmentsPage() {
  return (
    <section className="sentinel-card" aria-labelledby="env-heading">
      <h2 id="env-heading">Environments</h2>
      <p>
        Every project gets a default environment, classified <code>local</code> by default. You can
        change its classification (local / development / test / staging / production / unknown) from
        the <a href="/projects">Projects</a> page — production and unknown are handled restrictively:
        mutation, load-test, and active-security-scan permissions are forced off automatically and
        cannot be re-enabled from the same request.
      </p>
      <p className="sentinel-status-unknown">
        A dedicated cross-project environments view, base URLs, and credential references are planned
        for a later phase.
      </p>
    </section>
  );
}
