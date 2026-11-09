

const PageCredits = (() => {

  async function init() {
    await _loadCredits();
  }

  async function _loadCredits() {
    const set = (id, val) => {
      const el = document.getElementById(id);
      if (el) el.textContent = val || 'N/A';
    };

    try {
      const result = await API.loadConfig();
      if (!result.ok) return;

      const data = result;

      const setLink = (id, href) => {
        const el = document.getElementById(id);
        if (el && href) {
          el.href = href;
          el.style.display = 'inline-flex';
        } else if (el) {
          el.style.display = 'none';
        }
      };

      set('credit-site',    data.site    || data.websiteurl);
      set('credit-discord', data.discord || data.discordurl);
      set('credit-github',  data.github  || data.githuburl);
      set('credit-pseudo',  data.pseudo  || data.creditpseudo);

      setLink('credit-site-btn',    data.site    || data.websiteurl);
      setLink('credit-discord-btn', data.discord || data.discordurl);
      setLink('credit-github-btn',  data.github  || data.githuburl);

      State.saveCredits({
        site:    data.site    || data.websiteurl || '',
        discord: data.discord || data.discordurl || '',
        github:  data.github  || data.githuburl  || '',
        pseudo:  data.pseudo  || data.creditpseudo || '',
      });
    } catch (err) {

      const cached = State.getCredits();
      if (cached.site) {
        set('credit-site',    cached.site);
        set('credit-discord', cached.discord || 'N/A');
        set('credit-github',  cached.github  || 'N/A');
        set('credit-pseudo',  cached.pseudo  || 'N/A');
      }
    }
  }

  return { init };
})();
