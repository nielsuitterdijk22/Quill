// Route-level loading skeleton (Creem-style): the app is server-rendered and
// full-page loads used to just blank while data fetched. This shimmers in the
// page frame — title bar, cards, list rows — while the real content streams.
export default function AppLoading() {
  return (
    <>
      <div className="top">
        <div className="skeleton" style={{ width: 140, height: 22 }} />
      </div>
      <div className="cards">
        {[0, 1, 2, 3].map((i) => (
          <div className="card" key={i}>
            <div className="skeleton" style={{ width: 80, height: 12 }} />
            <div className="skeleton" style={{ width: 48, height: 24, marginTop: 10 }} />
          </div>
        ))}
      </div>
      <div className="panel">
        <div className="panel-toolbar" style={{ gap: 10 }}>
          <div className="skeleton" style={{ width: 120, height: 14 }} />
          <div className="spacer" />
          <div className="skeleton" style={{ width: 90, height: 26 }} />
        </div>
        {[0, 1, 2, 3, 4].map((i) => (
          <div className="row-item" key={i}>
            <div className="skeleton" style={{ width: 260, height: 13 }} />
            <span className="spacer" />
            <div className="skeleton" style={{ width: 60, height: 13 }} />
          </div>
        ))}
      </div>
    </>
  );
}
