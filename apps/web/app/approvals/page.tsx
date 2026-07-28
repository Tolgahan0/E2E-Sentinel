export default function ApprovalsPage() {
  return (
    <section className="sentinel-card" aria-labelledby="approvals-heading">
      <h2 id="approvals-heading">Approvals</h2>
      <p>
        Test case approvals are reviewed per-project on the <a href="/test-inventory">Test Inventory</a>{' '}
        page: approve, reject, or edit a suggested test there. Approving a mutating test is blocked
        while the project has a production or unknown-classified environment (see{' '}
        <a href="/environments">Environments</a>).
      </p>
      <p className="sentinel-status-unknown">
        A unified cross-project approval queue, plus time-bounded/revocable approvals for patches and
        repository writes, are planned for Phase 8 (Fix Proposals) and Phase 9.
      </p>
    </section>
  );
}
