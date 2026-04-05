package api

import "html/template"

var explorePageTemplate = template.Must(template.New("explore-page").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Explore Petitions - Vocdoni Passport</title>
  <style>
    * { box-sizing: border-box; }
    body { font-family: system-ui, -apple-system, sans-serif; background:#f3f4f6; color:#111827; margin:0; line-height:1.5; }
    .nav { background: #1f2937; padding: 0 20px; }
    .nav-inner { max-width: 1000px; margin: 0 auto; display: flex; align-items: center; justify-content: space-between; }
    .nav-brand { display: flex; align-items: center; gap: 10px; color: #fff; font-weight: 700; font-size: 18px; text-decoration: none; padding: 16px 0; }
    .nav-brand:hover { color: #e0e7ff; }
    .nav-links { display: flex; gap: 4px; }
    .nav-link { color: #d1d5db; text-decoration: none; padding: 10px 16px; border-radius: 6px; font-size: 14px; font-weight: 500; transition: all 0.15s; }
    .nav-link:hover { color: #fff; background: rgba(255,255,255,0.1); }
    .nav-link.active { color: #fff; background: #4f46e5; }
    .wrap { max-width: 1000px; margin: 0 auto; padding: 20px; }
    .card { background:#fff; border-radius:12px; padding:24px; box-shadow:0 1px 3px rgba(0,0,0,.08); margin-bottom:20px; }
    h1 { margin:0 0 8px 0; font-size: 28px; }
    h2 { margin:0 0 16px 0; font-size: 20px; font-weight: 700; }
    h3 { margin:16px 0 8px 0; font-size: 14px; color: #6b7280; }
    p { margin: 0 0 12px 0; }
    a { color: #4f46e5; }
    .muted { color:#6b7280; font-size:14px; }
    .btn { display:inline-flex; align-items:center; justify-content:center; padding:10px 16px; border-radius:8px; font-size:14px; font-weight:600; border:none; cursor:pointer; transition: all 0.15s; text-decoration: none; }
    .btn-primary { background:#4f46e5; color:#fff; }
    .btn-primary:hover { background:#4338ca; }
    .btn-secondary { background:#f3f4f6; color:#374151; border: 1px solid #d1d5db; }
    .btn-secondary:hover { background:#e5e7eb; }
    .btn-sm { padding: 6px 12px; font-size: 13px; }
    .page-header { margin-bottom: 24px; }
    .sort-bar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
    .sort-options { display: flex; gap: 8px; }
    .sort-btn { padding: 8px 16px; border-radius: 8px; font-size: 13px; font-weight: 500; border: 1px solid #d1d5db; background: #fff; cursor: pointer; transition: all 0.15s; text-decoration: none; color: #374151; }
    .sort-btn:hover { background: #f3f4f6; }
    .sort-btn.active { background: #4f46e5; color: #fff; border-color: #4f46e5; }
    .petition-grid { display: grid; gap: 16px; }
    .petition-card { background: #fff; border-radius: 12px; padding: 20px; box-shadow: 0 1px 3px rgba(0,0,0,.08); display: flex; justify-content: space-between; align-items: flex-start; transition: all 0.15s; border: 1px solid transparent; }
    .petition-card:hover { border-color: #c7d2fe; box-shadow: 0 4px 12px rgba(0,0,0,.1); }
    .petition-info { flex: 1; }
    .petition-name { font-size: 18px; font-weight: 700; color: #111827; margin: 0 0 4px 0; }
    .petition-purpose { font-size: 14px; color: #6b7280; margin: 0 0 12px 0; }
    .petition-meta { display: flex; gap: 16px; font-size: 13px; color: #9ca3af; }
    .petition-stat { display: flex; align-items: center; gap: 4px; }
    .petition-stat strong { color: #4f46e5; font-weight: 700; }
    .petition-actions { display: flex; flex-direction: column; gap: 8px; align-items: flex-end; }
    .sig-count { font-size: 28px; font-weight: 800; color: #4f46e5; line-height: 1; }
    .sig-label { font-size: 11px; color: #9ca3af; text-transform: uppercase; letter-spacing: 0.5px; }
    .empty-state { text-align: center; padding: 60px 20px; }
    .empty-icon { font-size: 64px; margin-bottom: 16px; }
  </style>
</head>
<body>
  <nav class="nav">
    <div class="nav-inner">
      <a href="/" class="nav-brand">🛂 Vocdoni Passport</a>
      <div class="nav-links">
        <a href="/" class="nav-link">Create</a>
        <a href="/explore" class="nav-link active">Explore</a>
        <a href="/about" class="nav-link">About</a>
      </div>
    </div>
  </nav>
  <div class="wrap">
    <div class="page-header">
      <h1>📋 Explore Petitions</h1>
      <p class="muted">Browse active petitions and see signature counts</p>
    </div>

    <div class="sort-bar">
      <span class="muted">{{.Total}} petition{{if ne .Total 1}}s{{end}} found</span>
      <div class="sort-options">
        <a href="/explore?sort=date" class="sort-btn {{if eq .SortBy "date"}}active{{end}}">📅 Newest</a>
        <a href="/explore?sort=signatures" class="sort-btn {{if eq .SortBy "signatures"}}active{{end}}">🔥 Most Signed</a>
      </div>
    </div>

    {{if .Petitions}}
    <div class="petition-grid">
      {{range .Petitions}}
      <div class="petition-card">
        <div class="petition-info">
          <h3 class="petition-name">{{.Name}}</h3>
          <p class="petition-purpose">{{.Purpose}}</p>
          <div class="petition-meta">
            <span class="petition-stat">📅 {{.CreatedAt.Format "Jan 2, 2006"}}</span>
            {{if .DisclosedFields}}
            <span class="petition-stat">📋 {{len .DisclosedFields}} fields</span>
            {{end}}
            {{if .Preset}}
            <span class="petition-stat">🏷️ {{.Preset}}</span>
            {{end}}
          </div>
        </div>
        <div class="petition-actions">
          <div style="text-align:center;">
            <div class="sig-count">{{.SignatureCount}}</div>
            <div class="sig-label">signatures</div>
          </div>
          <a href="/petition/{{.PetitionID}}" target="_blank" class="btn btn-primary btn-sm">View →</a>
        </div>
      </div>
      {{end}}
    </div>
    {{else}}
    <div class="card empty-state">
      <div class="empty-icon">📭</div>
      <h2>No petitions yet</h2>
      <p class="muted">Be the first to create a petition!</p>
      <a href="/" class="btn btn-primary" style="margin-top:16px;">Create Petition</a>
    </div>
    {{end}}
  </div>
</body>
</html>`))

var aboutPageTemplate = template.Must(template.New("about-page").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>About - Vocdoni Passport</title>
  <style>
    * { box-sizing: border-box; }
    body { font-family: system-ui, -apple-system, sans-serif; background:#f3f4f6; color:#111827; margin:0; line-height:1.5; }
    .nav { background: #1f2937; padding: 0 20px; }
    .nav-inner { max-width: 1000px; margin: 0 auto; display: flex; align-items: center; justify-content: space-between; }
    .nav-brand { display: flex; align-items: center; gap: 10px; color: #fff; font-weight: 700; font-size: 18px; text-decoration: none; padding: 16px 0; }
    .nav-brand:hover { color: #e0e7ff; }
    .nav-links { display: flex; gap: 4px; }
    .nav-link { color: #d1d5db; text-decoration: none; padding: 10px 16px; border-radius: 6px; font-size: 14px; font-weight: 500; transition: all 0.15s; }
    .nav-link:hover { color: #fff; background: rgba(255,255,255,0.1); }
    .nav-link.active { color: #fff; background: #4f46e5; }
    .wrap { max-width: 1000px; margin: 0 auto; padding: 20px; }
    .card { background:#fff; border-radius:12px; padding:24px; box-shadow:0 1px 3px rgba(0,0,0,.08); margin-bottom:20px; }
    h1 { margin:0 0 8px 0; font-size: 28px; }
    h2 { margin:0 0 16px 0; font-size: 20px; font-weight: 700; display: flex; align-items: center; gap: 10px; }
    h3 { margin:16px 0 8px 0; font-size: 14px; color: #6b7280; }
    p { margin: 0 0 12px 0; color: #4b5563; line-height: 1.7; }
    a { color: #4f46e5; }
    .muted { color:#6b7280; font-size:14px; }
    .hero { text-align: center; padding: 40px 20px; }
    .hero h1 { font-size: 36px; margin-bottom: 16px; }
    .hero p { font-size: 18px; color: #6b7280; max-width: 600px; margin: 0 auto; }
    .features { display: grid; grid-template-columns: repeat(3, 1fr); gap: 20px; margin: 40px 0; }
    .feature { background: #fff; border-radius: 12px; padding: 24px; text-align: center; box-shadow: 0 1px 3px rgba(0,0,0,.08); }
    .feature-icon { font-size: 48px; margin-bottom: 16px; }
    .feature h3 { font-size: 18px; font-weight: 700; margin: 0 0 8px 0; color: #111827; }
    .feature p { font-size: 14px; color: #6b7280; margin: 0; }
    .section { margin: 40px 0; }
    .tech-list { display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px; margin-top: 16px; }
    .tech-item { background: #f9fafb; border-radius: 8px; padding: 12px 16px; display: flex; align-items: center; gap: 10px; }
    .tech-item span { font-size: 20px; }
    .tech-item strong { font-size: 14px; color: #374151; }
    .footer { text-align: center; padding: 40px 20px; border-top: 1px solid #e5e7eb; margin-top: 40px; }
    .footer-logo { font-size: 14px; color: #6b7280; }
    .footer-logo a { color: #4f46e5; font-weight: 600; }
    @media (max-width: 768px) { .features { grid-template-columns: 1fr; } .tech-list { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <nav class="nav">
    <div class="nav-inner">
      <a href="/" class="nav-brand">🛂 Vocdoni Passport</a>
      <div class="nav-links">
        <a href="/" class="nav-link">Create</a>
        <a href="/explore" class="nav-link">Explore</a>
        <a href="/about" class="nav-link active">About</a>
      </div>
    </div>
  </nav>
  <div class="wrap">
    <div class="hero">
      <h1>🛂 Vocdoni Passport</h1>
      <p>Privacy-preserving identity verification using zero-knowledge proofs and your government-issued ID</p>
    </div>

    <div class="features">
      <div class="feature">
        <div class="feature-icon">🔐</div>
        <h3>Zero-Knowledge Proofs</h3>
        <p>Prove facts about yourself without revealing your actual data</p>
      </div>
      <div class="feature">
        <div class="feature-icon">🛡️</div>
        <h3>Privacy First</h3>
        <p>Your personal information never leaves your device</p>
      </div>
      <div class="feature">
        <div class="feature-icon">✅</div>
        <h3>Cryptographically Verified</h3>
        <p>Proofs are mathematically verified, not trusted</p>
      </div>
    </div>

    <div class="card section">
      <h2>🤔 What is this?</h2>
      <p>
        Vocdoni Passport is a revolutionary system that allows you to prove things about your identity 
        (like your nationality, age, or that you hold a valid government ID) without revealing your 
        actual personal information.
      </p>
      <p>
        Using the NFC chip in your passport or national ID card, the mobile app reads your identity 
        data locally on your phone. It then generates a <strong>zero-knowledge proof</strong> — a 
        cryptographic proof that demonstrates certain facts are true without exposing the underlying data.
      </p>
    </div>

    <div class="card section">
      <h2>⚙️ How does it work?</h2>
      <p>The process involves several steps, all designed to protect your privacy:</p>
      <div class="tech-list">
        <div class="tech-item">
          <span>📱</span>
          <strong>1. Scan your ID with NFC</strong>
        </div>
        <div class="tech-item">
          <span>🔒</span>
          <strong>2. Data stays on your device</strong>
        </div>
        <div class="tech-item">
          <span>⚡</span>
          <strong>3. Generate ZK proof locally</strong>
        </div>
        <div class="tech-item">
          <span>☁️</span>
          <strong>4. Server verifies the proof</strong>
        </div>
      </div>
      <p style="margin-top:16px;">
        The server never sees your name, document number, photo, or any other personal data. 
        It only receives a mathematical proof that your claims are valid, signed by your 
        government's certificate authority.
      </p>
    </div>

    <div class="card section">
      <h2>🛡️ Why is it safe?</h2>
      <p>
        <strong>Your data never leaves your phone.</strong> The NFC chip data is read locally and 
        processed entirely on your device. Only the zero-knowledge proof is transmitted.
      </p>
      <p>
        <strong>Cryptographic guarantees.</strong> Zero-knowledge proofs are based on advanced 
        cryptography (specifically, zkSNARKs). It's mathematically impossible to extract your 
        personal data from the proof.
      </p>
      <p>
        <strong>Government-backed verification.</strong> The proof verifies that your ID was 
        genuinely signed by your country's certificate authority, preventing forgery.
      </p>
      <p>
        <strong>Nullifier prevents double-signing.</strong> Each signature generates a unique 
        nullifier that prevents the same ID from signing the same petition twice, without 
        revealing your identity.
      </p>
    </div>

    <div class="card section">
      <h2>🔏 Privacy guarantees</h2>
      <p>When you sign a petition, the verifier learns <strong>only</strong> what you choose to disclose:</p>
      <div class="tech-list">
        <div class="tech-item">
          <span>✓</span>
          <strong>You hold a valid government ID</strong>
        </div>
        <div class="tech-item">
          <span>✓</span>
          <strong>Selected attributes (e.g., nationality, age range)</strong>
        </div>
        <div class="tech-item">
          <span>✗</span>
          <strong>Your name — NOT revealed</strong>
        </div>
        <div class="tech-item">
          <span>✗</span>
          <strong>Your document number — NOT revealed</strong>
        </div>
        <div class="tech-item">
          <span>✗</span>
          <strong>Your photo — NOT revealed</strong>
        </div>
        <div class="tech-item">
          <span>✗</span>
          <strong>Your address — NOT revealed</strong>
        </div>
      </div>
    </div>

    <div class="card section">
      <h2>🔧 Technology</h2>
      <p>Vocdoni Passport is built on cutting-edge cryptographic technology:</p>
      <div class="tech-list">
        <div class="tech-item">
          <span>🔮</span>
          <strong>zkPassport — ZK circuits for passport verification</strong>
        </div>
        <div class="tech-item">
          <span>⚡</span>
          <strong>Barretenberg — High-performance proving system</strong>
        </div>
        <div class="tech-item">
          <span>📜</span>
          <strong>ICAO 9303 — International passport standard</strong>
        </div>
        <div class="tech-item">
          <span>📱</span>
          <strong>React Native — Cross-platform mobile app</strong>
        </div>
      </div>
    </div>

    <div class="footer">
      <p class="footer-logo">
        Developed with ❤️ by <a href="https://vocdoni.io" target="_blank">Vocdoni.io</a>
      </p>
      <p class="muted" style="margin-top:8px;">
        Building the future of digital democracy and privacy-preserving identity
      </p>
    </div>
  </div>
</body>
</html>`))
