(() => {
  const ROUTES = {
    '': 'getting-started',
    'getting-started': 'getting-started',
    architecture: 'architecture',
    accounts: 'accounts',
    transactions: 'transactions',
    privacy: 'privacy',
    sse: 'sse',
    consensus: 'consensus',
    'node-ops': 'node-ops',
    reference: 'reference',
  };

  const content = document.getElementById('content');
  const navLinks = [...document.querySelectorAll('.nav a[data-route]')];

  function routeFromHash() {
    const raw = (location.hash || '#/getting-started').replace(/^#\/?/, '');
    const key = raw.split('?')[0].split('/')[0];
    return ROUTES[key] || 'getting-started';
  }

  function setActive(route) {
    navLinks.forEach((a) => {
      a.classList.toggle('active', a.dataset.route === route);
    });
  }

  function closeNav() {
    document.body.classList.remove('nav-open');
  }

  function waitForMermaid(timeoutMs = 3000) {
    if (window.mermaid) return Promise.resolve(window.mermaid);
    return new Promise((resolve) => {
      const start = Date.now();
      const t = setInterval(() => {
        if (window.mermaid) {
          clearInterval(t);
          resolve(window.mermaid);
        } else if (Date.now() - start > timeoutMs) {
          clearInterval(t);
          resolve(null);
        }
      }, 50);
    });
  }

  async function loadRoute() {
    const route = routeFromHash();
    setActive(route);
    closeNav();

    if (route === 'reference') {
      content.className = 'content full-bleed';
      content.innerHTML =
        '<iframe title="API Reference" src="reference.html" style="border:0;width:100%;height:100%;display:block"></iframe>';
      document.title = 'API Reference · Pixie Block';
      return;
    }

    content.className = 'content';
    content.innerHTML = '<p class="loading">carregando…</p>';

    try {
      const res = await fetch(`pages/${route}.html`, { cache: 'no-cache' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const html = await res.text();
      content.innerHTML = `<article class="article">${html}</article>`;
      const h1 = content.querySelector('h1');
      document.title = h1 ? `${h1.textContent} · Pixie Block` : 'Pixie Block Docs';
      const mermaid = await waitForMermaid();
      if (mermaid) {
        content.querySelectorAll('.mermaid').forEach((el, i) => {
          el.removeAttribute('data-processed');
          if (!el.id) el.id = `mermaid-${route}-${i}`;
        });
        await mermaid.run({ nodes: content.querySelectorAll('.mermaid') });
      }
    } catch (err) {
      content.innerHTML = `<p class="error">Falha ao carregar a página: ${err.message}</p>`;
    }
  }

  document.getElementById('menu-toggle')?.addEventListener('click', () => {
    document.body.classList.toggle('nav-open');
  });
  document.querySelector('.sidebar-backdrop')?.addEventListener('click', closeNav);

  window.addEventListener('hashchange', loadRoute);
  if (!location.hash) location.hash = '#/getting-started';
  else loadRoute();
})();
