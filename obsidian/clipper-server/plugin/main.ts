import {
  App,
  Modal,
  Notice,
  Plugin,
  PluginSettingTab,
  Setting,
  TFile,
  normalizePath,
  requestUrl,
} from 'obsidian';

interface ClipperSettings {
  serverUrl: string;
  token: string;
}

const DEFAULT_SETTINGS: ClipperSettings = { serverUrl: 'http://localhost:8090', token: '' };

export default class ClipperClient extends Plugin {
  settings: ClipperSettings = DEFAULT_SETTINGS;

  async onload() {
    this.settings = Object.assign({}, DEFAULT_SETTINGS, await this.loadData());
    this.addCommand({
      id: 'clip-url',
      name: 'Clip URL…',
      callback: () => new ClipModal(this.app, this).open(),
    });
    this.addSettingTab(new ClipperSettingTab(this.app, this));
  }

  async saveSettings() {
    await this.saveData(this.settings);
  }

  async clip(url: string) {
    const notice = new Notice('Clipping…', 0);
    try {
      const res = await requestUrl({
        url: this.settings.serverUrl.replace(/\/+$/, '') + '/clip',
        method: 'POST',
        contentType: 'application/json',
        body: JSON.stringify({ url }),
        headers: this.settings.token ? { authorization: `Bearer ${this.settings.token}` } : {},
        throw: false,
      });
      if (res.status !== 200) {
        throw new Error(res.json?.error ?? `server returned ${res.status}`);
      }
      const { noteName, path, content } = res.json;
      const file = await this.createNote(path, noteName, content);
      notice.setMessage(`Clipped to ${file.path}`);
      window.setTimeout(() => notice.hide(), 3000);
      await this.app.workspace.getLeaf().openFile(file);
    } catch (e: any) {
      notice.hide();
      new Notice(`Clip failed: ${e?.message ?? e}`, 8000);
    }
  }

  async createNote(folder: string, name: string, content: string): Promise<TFile> {
    if (folder && !this.app.vault.getAbstractFileByPath(normalizePath(folder))) {
      await this.app.vault.createFolder(normalizePath(folder));
    }
    for (let i = 0; ; i++) {
      const candidate = normalizePath(`${folder ? folder + '/' : ''}${name}${i ? ` ${i}` : ''}.md`);
      if (!this.app.vault.getAbstractFileByPath(candidate)) {
        return this.app.vault.create(candidate, content);
      }
    }
  }
}

class ClipModal extends Modal {
  constructor(
    app: App,
    private plugin: ClipperClient
  ) {
    super(app);
  }

  onOpen() {
    this.titleEl.setText('Clip URL');
    const input = this.contentEl.createEl('input', {
      type: 'text',
      placeholder: 'https://…',
    });
    input.style.width = '100%';
    navigator.clipboard
      ?.readText?.()
      .then((t) => {
        if (/^https?:\/\//.test(t.trim()) && !input.value) input.value = t.trim();
      })
      .catch(() => {});
    const go = () => {
      const url = input.value.trim();
      if (!url) return;
      this.close();
      void this.plugin.clip(url);
    };
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') go();
    });
    const btn = this.contentEl.createEl('button', { text: 'Clip' });
    btn.style.marginTop = '8px';
    btn.addEventListener('click', go);
    input.focus();
  }

  onClose() {
    this.contentEl.empty();
  }
}

class ClipperSettingTab extends PluginSettingTab {
  constructor(
    app: App,
    private plugin: ClipperClient
  ) {
    super(app, plugin);
  }

  display() {
    this.containerEl.empty();
    new Setting(this.containerEl)
      .setName('Server URL')
      .setDesc('Where clipper-server is reachable, e.g. http://homeserver:8090')
      .addText((t) =>
        t.setValue(this.plugin.settings.serverUrl).onChange(async (v) => {
          this.plugin.settings.serverUrl = v.trim();
          await this.plugin.saveSettings();
        })
      );
    new Setting(this.containerEl)
      .setName('Token')
      .setDesc('Optional bearer token (CLIPPER_TOKEN on the server)')
      .addText((t) =>
        t.setValue(this.plugin.settings.token).onChange(async (v) => {
          this.plugin.settings.token = v.trim();
          await this.plugin.saveSettings();
        })
      );
  }
}
