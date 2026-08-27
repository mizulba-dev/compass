import type { Policy } from '../types';

export function PolicyList({ policies }: { policies: Policy[] }) {
  return (
    <section className="card">
      <h2>Policies</h2>
      <p className="empty-hint" style={{ marginBottom: policies.length ? 8 : 0 }}>
        Decisions you've made, that the agent follows going forward.
      </p>
      {policies.length > 0 && (
        <ul className="policy-list">
          {policies.map((policy) => (
            <li key={policy.id} className="policy-item">
              {policy.text}
              {policy.derivedFrom && <span className="policy-derived">from: {policy.derivedFrom}</span>}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
