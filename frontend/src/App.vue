<script setup lang="ts">
import {
  ref,
  computed,
  onMounted,
  onBeforeUnmount,
  watch,
  nextTick,
} from "vue";
import draggable from "vuedraggable";
import {
  PhHouseLine,
  PhMagnifyingGlass,
  PhPlus,
  PhGearSix,
  PhGlobeHemisphereWest,
  PhSun,
  PhMoon,
  PhTranslate,
  PhList,
  PhX,
  PhPencilSimple,
  PhDotsThree,
  PhFolderSimple,
  PhArrowSquareOut,
  PhDownloadSimple,
  PhUploadSimple,
  PhHardDrives,
  PhLockKey,
  PhSignOut,
  PhCheck,
  PhDotsSixVertical,
  PhArrowClockwise,
  PhImage,
  PhInfo,
  PhArrowLeft,
  PhTrash,
  PhCards,
  PhSquaresFour,
  PhLink,
} from "@phosphor-icons/vue";
import type { Config, Site, Group, Candidate } from "./types";
const failedIcons = ref(new Set<string>());
const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value));
const lang = ref(localStorage.getItem("nest.lang") || "zh");
const tr = (zh: string, en: string) => (lang.value === "zh" ? zh : en);
const session = ref({ setup: false, authenticated: false, version: "0.1.0" });
const config = ref<Config | null>(null);
const loading = ref(true);
const pending = ref(false);
const error = ref("");
const toast = ref("");
let toastTimer: ReturnType<typeof setTimeout>;
const query = ref("");
const active = ref("all");
const edit = ref(false);
const menu = ref(false);
const settings = ref("");
const modal = ref("");
const focusReturn = ref<HTMLElement | null>(null);
const dialog = ref<HTMLElement | null>(null);
const username = ref("admin");
const password = ref("");
const currentPassword = ref("");
const newPassword = ref("");
const backupPassword = ref("");
const backgroundURL = ref("");
const backgroundStatus = ref({ configured: false, host: "" });
const site = ref<Site>({
  id: "",
  title: "",
  url: "",
  description: "",
  icon: "",
});
const siteGroup = ref("");
const groupTitle = ref("");
const groupID = ref("");
const fetching = ref(false);
const importData = ref<any>(null);
const preview = ref<any>(null);
const importMode = ref("merge");
const backupFile = ref<any>(null);
const fileName = ref("");
const dockerHost = ref(location.hostname);
const candidates = ref<Candidate[]>([]);
const selected = ref<string[]>([]);
const dockerGroup = ref("");
const finding = ref(false);
const themeDraft = ref<Config["theme"] | null>(null);
const autoFilling = ref(false);
const progress = ref("");
let cancelled = false;
const systemDark = ref(matchMedia("(prefers-color-scheme: dark)").matches);
const media = matchMedia("(prefers-color-scheme: dark)");
const mediaChange = (e: MediaQueryListEvent) => (systemDark.value = e.matches);
const theme = computed(() =>
  settings.value === "theme" && themeDraft.value
    ? themeDraft.value
    : config.value?.theme,
);
const palette = computed(() =>
  theme.value?.palette === "system"
    ? systemDark.value
      ? "graphite"
      : "ivory"
    : theme.value?.palette || "graphite",
);
const visible = computed(() => {
  const q = query.value.trim().toLowerCase();
  return (config.value?.groups || [])
    .filter((g) => active.value === "all" || active.value === g.id)
    .map((g) => ({
      ...g,
      sites: g.sites.filter(
        (s) =>
          !q ||
          `${s.title} ${s.url} ${s.description}`.toLowerCase().includes(q),
      ),
    }))
    .filter((g) => edit.value || g.sites.length || !q);
});
const canDrag = computed(
  () => edit.value && !query.value && !pending.value && !autoFilling.value,
);
const total = computed(
  () => config.value?.groups.reduce((n, g) => n + g.sites.length, 0) || 0,
);
const nav = computed(() => [
  { id: "sites", zh: "网站管理", en: "Websites", icon: PhLink },
  { id: "groups", zh: "分组管理", en: "Groups", icon: PhFolderSimple },
  { id: "docker", zh: "容器发现", en: "Docker", icon: PhHardDrives },
  { id: "theme", zh: "主题外观", en: "Appearance", icon: PhSun },
  { id: "security", zh: "访问安全", en: "Security", icon: PhLockKey },
  { id: "backup", zh: "备份恢复", en: "Backup", icon: PhDownloadSimple },
  { id: "about", zh: "关于项目", en: "About", icon: PhInfo },
]);
const notice = (s: string) => {
  toast.value = s;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (toast.value = ""), 4500);
};
async function api(path: string, method = "GET", body?: any) {
  const resp = await fetch("/api/" + path, {
    method,
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "NestPanel",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await resp.json();
  if (!resp.ok) {
    if (resp.status === 401 && session.value.authenticated) {
      session.value.authenticated = false;
      settings.value = "";
      edit.value = false;
    }
    throw new Error(data.error || `HTTP ${resp.status}`);
  }
  return data;
}
async function run(fn: () => Promise<void>) {
  if (pending.value) return;
  pending.value = true;
  error.value = "";
  try {
    await fn();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    pending.value = false;
  }
}
async function load() {
  loading.value = true;
  error.value = "";
  try {
    session.value = await api("session");
    if (!session.value.setup) {
      try {
        config.value = await api("panel");
        document.title = `${config.value!.name} · Nest Panel`;
      } catch (e) {
        if (session.value.authenticated) throw e;
        config.value = null;
      }
    }
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
async function login() {
  await run(async () => {
    await api(session.value.setup ? "setup" : "login", "POST", {
      username: username.value,
      password: password.value,
    });
    password.value = "";
    await load();
  });
}
async function save(c: Config | null = config.value) {
  if (!c) return;
  config.value = await api("panel", "PUT", c);
  document.title = `${config.value!.name} · Nest Panel`;
  notice(tr("已保存", "Saved"));
}
async function saveGuard() {
  await run(() => save());
}
async function logout() {
  await run(async () => {
    await api("logout", "POST", {});
    settings.value = "";
    edit.value = false;
    await load();
  });
}
function openModal(type: string) {
  error.value = "";
  focusReturn.value = document.activeElement as HTMLElement;
  modal.value = type;
  nextTick(() =>
    dialog.value?.querySelector<HTMLElement>("input,button,select")?.focus(),
  );
}
function closeModal() {
  if (pending.value || fetching.value) return;
  modal.value = "";
  error.value = "";
  nextTick(() => focusReturn.value?.focus());
}
function openSettings(page = "sites") {
  error.value = "";
  settings.value = page;
  menu.value = false;
  if (page === "theme") {
    themeDraft.value = clone(config.value!.theme);
    api("background")
      .then((v) => (backgroundStatus.value = v))
      .catch((e) => (error.value = e.message));
  }
}
function leaveSettings() {
  settings.value = "";
  themeDraft.value = null;
  error.value = "";
  cancelled = true;
}
function changeSettings(page: string) {
  if (
    settings.value === "theme" &&
    JSON.stringify(themeDraft.value) !== JSON.stringify(config.value?.theme) &&
    !confirm(
      tr("放弃未保存的主题修改？", "Discard unsaved appearance changes?"),
    )
  )
    return;
  openSettings(page);
}
function newSite(group?: string) {
  site.value = {
    id: crypto.randomUUID(),
    title: "",
    url: "",
    description: "",
    icon: "",
  };
  siteGroup.value = group || config.value?.groups[0]?.id || "";
  openModal("site");
}
function editSite(s: Site, g: string) {
  site.value = { ...s };
  siteGroup.value = g;
  openModal("site");
}
async function getMetadata() {
  if (fetching.value || !site.value.url) return;
  const requestURL = site.value.url;
  const oldTitle = site.value.title;
  const oldIcon = site.value.icon;
  fetching.value = true;
  error.value = "";
  try {
    const result = await api("metadata", "POST", { url: requestURL });
    if (site.value.url === requestURL) {
      if (site.value.title === oldTitle) site.value.title = result.title;
      if (site.value.icon === oldIcon) site.value.icon = result.icon;
      notice(
        result.warnings?.length
          ? result.warnings.join(" · ")
          : tr("已获取标题和图标", "Title and icon retrieved"),
      );
    }
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    fetching.value = false;
  }
}
async function saveSite() {
  await run(async () => {
    if (!config.value || !siteGroup.value)
      throw new Error(tr("请先创建分组", "Create a group first"));
    const next = clone(config.value);
    let previousIndex = -1;
    for (const g of next.groups) {
      if (g.id === siteGroup.value)
        previousIndex = g.sites.findIndex((s) => s.id === site.value.id);
      g.sites = g.sites.filter((s) => s.id !== site.value.id);
    }
    const target = next.groups.find((g) => g.id === siteGroup.value)!;
    target.sites.splice(
      previousIndex < 0 ? target.sites.length : previousIndex,
      0,
      { ...site.value },
    );
    await save(next);
    modal.value = "";
  });
}
function newGroup(g?: Group) {
  groupID.value = g?.id || "";
  groupTitle.value = g?.title || "";
  openModal("group");
}
async function saveGroup() {
  await run(async () => {
    const next = clone(config.value!);
    const g = next.groups.find((g) => g.id === groupID.value);
    if (g) g.title = groupTitle.value;
    else
      next.groups.push({
        id: crypto.randomUUID(),
        title: groupTitle.value,
        sites: [],
      });
    await save(next);
    modal.value = "";
  });
}
async function removeSite(s: Site) {
  if (!confirm(tr(`移除“${s.title}”？`, `Remove “${s.title}”?`))) return;
  await run(async () => {
    const next = clone(config.value!);
    for (const g of next.groups) g.sites = g.sites.filter((x) => x.id !== s.id);
    await save(next);
  });
}
async function removeGroup(g: Group) {
  if (
    !confirm(
      tr(
        `移除“${g.title}”分组及其中的 ${g.sites.length} 个网站？`,
        `Remove “${g.title}” and its ${g.sites.length} websites?`,
      ),
    )
  )
    return;
  await run(async () => {
    const next = clone(config.value!);
    next.groups = next.groups.filter((x) => x.id !== g.id);
    await save(next);
    active.value = "all";
  });
}
function domain(s: string) {
  try {
    return new URL(s).host;
  } catch {
    return s;
  }
}
function iconURL(id: string) {
  return "/api/icons/" + id;
}
function fileInput(e: Event) {
  return (e.target as HTMLInputElement).files?.[0];
}
async function upload(e: Event, background = false) {
  const file = fileInput(e);
  if (!file) return;
  await run(async () => {
    if (file.size > 8 * 1024 * 1024)
      throw new Error(tr("图片不能超过8MB", "Image must be under 8 MB"));
    const data = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.onerror = () => reject(new Error("Cannot read image"));
      reader.readAsDataURL(file);
    });
    const result = await api("image", "POST", { data, background });
    if (background && themeDraft.value) themeDraft.value.background = result.id;
    else site.value.icon = result.id;
  });
  (e.target as HTMLInputElement).value = "";
}
async function selectImport(e: Event) {
  const file = fileInput(e);
  if (!file) return;
  await run(async () => {
    if (file.size > 48 * 1024 * 1024)
      throw new Error(tr("文件不能超过48MB", "File must be under 48 MB"));
    importData.value = JSON.parse(await file.text());
    preview.value = await api("import/preview", "POST", importData.value);
    fileName.value = file.name;
  });
  (e.target as HTMLInputElement).value = "";
}
async function doImport() {
  if (
    importMode.value === "replace" &&
    !confirm(
      tr(
        "替换当前全部网站、分组和主题？",
        "Replace all current websites, groups and appearance?",
      ),
    )
  )
    return;
  await run(async () => {
    await api("import", "POST", {
      data: importData.value,
      mode: importMode.value,
      revision: config.value!.revision,
    });
    await load();
    preview.value = null;
    importData.value = null;
    notice(tr("导入完成", "Import complete"));
  });
}
async function download(path: string, name: string, body?: any) {
  const resp = await fetch("/api/" + path, {
    method: body ? "POST" : "GET",
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "NestPanel",
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!resp.ok) {
    const data = await resp.json();
    throw new Error(data.error || "Download failed");
  }
  const url = URL.createObjectURL(await resp.blob());
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
async function selectBackup(e: Event) {
  const file = fileInput(e);
  if (!file) return;
  await run(async () => {
    if (file.size > 64 * 1024 * 1024) throw new Error("Backup exceeds 64 MB");
    backupFile.value = JSON.parse(await file.text());
    fileName.value = file.name;
  });
  (e.target as HTMLInputElement).value = "";
}
async function restore() {
  if (
    !confirm(
      tr(
        "完整恢复会替换所有配置、图片和账户，并退出所有登录。确定继续？",
        "Restore replaces all configuration, images and account, and signs out all sessions. Continue?",
      ),
    )
  )
    return;
  await run(async () => {
    await api("restore", "POST", {
      backup: backupFile.value,
      password: backupPassword.value,
      revision: config.value!.revision,
    });
    backupFile.value = null;
    backupPassword.value = "";
    settings.value = "";
    await load();
    notice(
      tr(
        "已恢复，请使用备份中的账号登录",
        "Restored. Sign in using the account from the backup",
      ),
    );
  });
}
async function applyBackground(clear = false) {
  await run(async () => {
    const next = await api("background", "POST", {
      url: backgroundURL.value,
      clear,
      revision: config.value!.revision,
    });
    config.value = next;
    if (themeDraft.value) themeDraft.value.background = next.theme.background;
    backgroundURL.value = "";
    backgroundStatus.value = await api("background");
    notice(
      tr(
        clear ? "已移除API来源，保留当前图片" : "API背景已获取并保存",
        clear
          ? "API source removed; current image retained"
          : "API background fetched and saved",
      ),
    );
  });
}
async function findDocker() {
  if (finding.value) return;
  finding.value = true;
  error.value = "";
  try {
    candidates.value = await api(
      "docker?host=" + encodeURIComponent(dockerHost.value),
    );
    selected.value = candidates.value
      .filter((x) => !x.existing)
      .map((x) => x.id);
    dockerGroup.value = config.value?.groups[0]?.id || "";
    if (!candidates.value.length)
      notice(
        tr(
          "未发现有映射端口的运行中容器",
          "No running containers with published ports found",
        ),
      );
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    finding.value = false;
  }
}
async function addDocker() {
  await run(async () => {
    const next = clone(config.value!);
    let group = next.groups.find((x) => x.id === dockerGroup.value);
    if (!group) {
      group = { id: crypto.randomUUID(), title: "Docker", sites: [] };
      next.groups.push(group);
    }
    const seen = new Set(
      next.groups.flatMap((g) => g.sites.map((s) => s.url.replace(/\/$/, ""))),
    );
    for (const c of candidates.value.filter((c) =>
      selected.value.includes(c.id),
    )) {
      if (!seen.has(c.url.replace(/\/$/, ""))) {
        group.sites.push({
          id: crypto.randomUUID(),
          title: c.name,
          url: c.url,
          description: "",
          icon: "",
        });
        seen.add(c.url.replace(/\/$/, ""));
      }
    }
    await save(next);
    candidates.value = [];
    selected.value = [];
  });
}
async function fillIcons() {
  if (autoFilling.value) return;
  autoFilling.value = true;
  cancelled = false;
  error.value = "";
  const list = config
    .value!.groups.flatMap((g) => g.sites)
    .filter((s) => !s.icon);
  let completed = 0;
  try {
    for (const s of list) {
      if (cancelled) break;
      progress.value = `${++completed} / ${list.length}`;
      try {
        const data = await api("metadata", "POST", { url: s.url });
        const live = config.value?.groups
          .flatMap((g) => g.sites)
          .find((x) => x.id === s.id);
        if (live && data.icon) live.icon = data.icon;
      } catch {
        /* failed sites keep the bundled fallback */
      }
      if (completed % 10 === 0 || completed === list.length || cancelled)
        await save();
    }
    notice(
      tr(
        "图标获取已结束，失败项保留默认图标",
        "Icon fetch finished. Unavailable icons keep the default",
      ),
    );
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    autoFilling.value = false;
    progress.value = "";
  }
}
async function changePassword() {
  await run(async () => {
    await api("password", "POST", {
      current: currentPassword.value,
      username: username.value,
      password: newPassword.value,
    });
    currentPassword.value = "";
    newPassword.value = "";
    settings.value = "";
    await load();
    notice(
      tr("凭据已更新，请重新登录", "Credentials updated. Please sign in again"),
    );
  });
}
function keydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
    e.preventDefault();
    document.querySelector<HTMLInputElement>("#search")?.focus();
  }
  if (e.key === "Escape") {
    if (modal.value) closeModal();
    else menu.value = false;
  }
  if (e.key === "Tab" && modal.value && dialog.value) {
    const els = Array.from(
      dialog.value.querySelectorAll<HTMLElement>(
        "button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea,a[href]",
      ),
    );
    if (!els.length) return;
    const first = els[0],
      last = els[els.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }
}
watch(lang, (v) => {
  localStorage.setItem("nest.lang", v);
  document.documentElement.lang = v === "zh" ? "zh-CN" : "en";
});
watch(modal, (v) => (document.body.style.overflow = v ? "hidden" : ""));
onMounted(() => {
  load();
  media.addEventListener("change", mediaChange);
  document.addEventListener("keydown", keydown);
});
onBeforeUnmount(() => {
  cancelled = true;
  clearTimeout(toastTimer);
  media.removeEventListener("change", mediaChange);
  document.removeEventListener("keydown", keydown);
});
</script>

<template>
  <div
    class="app"
    :data-palette="palette"
    :data-layout="theme?.layout || 'cards'"
    :data-icons="theme?.icons || 'original'"
    :data-density="theme?.density || 'standard'"
    :class="{ 'has-background': !!theme?.background }"
    :style="{
      '--surface-opacity': (theme?.opacity || 94) / 100,
      '--background-blur': `${theme?.blur || 0}px`,
    }"
  >
    <div
      v-if="theme?.background"
      class="wallpaper"
      :style="{ backgroundImage: `url(${iconURL(theme.background)})` }"
    ></div>
    <div class="shell">
      <header class="topbar">
        <a
          class="brand"
          href="/"
          @click.prevent="
            leaveSettings();
            active = 'all';
          "
          ><PhHouseLine :size="28" weight="duotone" /><span>{{
            config?.name || "栖页"
          }}</span></a
        >
        <div v-if="config && !settings" class="search">
          <PhMagnifyingGlass :size="19" /><input
            id="search"
            v-model="query"
            :placeholder="
              tr(
                '搜索网站、域名或关键词',
                'Search websites, domains or keywords',
              )
            "
          /><kbd>⌘ K</kbd>
        </div>
        <span v-if="settings" class="header-label">{{
          nav.find((n) => n.id === settings)?.[lang === "zh" ? "zh" : "en"]
        }}</span>
        <div class="top-actions">
          <button
            class="icon-button"
            @click="lang = lang === 'zh' ? 'en' : 'zh'"
            :title="tr('Switch to English', '切换为中文')"
          >
            <PhTranslate :size="20" /><span class="language">{{
              lang === "zh" ? "中" : "EN"
            }}</span></button
          ><button
            v-if="session.authenticated && config"
            class="icon-button"
            @click="openSettings('theme')"
            :title="tr('主题设置', 'Appearance')"
          >
            <PhMoon
              v-if="palette === 'graphite' || palette === 'ocean'"
              :size="20"
            /><PhSun v-else :size="20" /></button
          ><button
            v-if="session.authenticated && config && !settings"
            class="icon-button"
            @click="openSettings()"
            :title="tr('管理设置', 'Settings')"
          >
            <PhGearSix :size="21" /></button
          ><button
            v-if="config && session.authenticated && !settings"
            class="primary add-top"
            @click="newSite()"
          >
            <PhPlus :size="18" />{{ tr("添加网站", "Add website") }}</button
          ><button
            v-if="config && !session.authenticated"
            class="small"
            @click="openModal('login')"
          >
            <PhLockKey :size="16" />{{ tr("登录", "Sign in") }}</button
          ><button v-if="settings" class="small" @click="leaveSettings">
            <PhArrowLeft :size="17" />{{ tr("返回导航", "Home") }}
          </button>
        </div>
      </header>
      <div v-if="loading" class="center-state">
        {{ tr("正在载入…", "Loading…") }}
      </div>
      <main v-else-if="!config" class="login-page">
        <div class="login-panel">
          <PhHouseLine :size="36" weight="duotone" />
          <h1>
            {{
              session.setup
                ? tr("创建你的导航", "Create your panel")
                : tr("欢迎回来", "Welcome back")
            }}
          </h1>
          <p>
            {{
              session.setup
                ? tr(
                    "设置唯一的管理员账户，开始整理常用网站。",
                    "Create your administrator account to get started.",
                  )
                : tr(
                    "登录后访问你的导航面板。",
                    "Sign in to access your navigation panel.",
                  )
            }}
          </p>
          <form @submit.prevent="login">
            <label
              >{{ tr("账户名称", "Username")
              }}<input
                v-model="username"
                autocomplete="username"
                required
                maxlength="64" /></label
            ><label
              >{{ tr("密码", "Password")
              }}<input
                v-model="password"
                type="password"
                :autocomplete="
                  session.setup ? 'new-password' : 'current-password'
                "
                required
                :minlength="session.setup ? 12 : 1"
                maxlength="72" /></label
            ><small v-if="session.setup">{{
              tr(
                "至少12位，包含大小写字母、数字和符号。",
                "At least 12 characters with uppercase, lowercase, number and symbol.",
              )
            }}</small>
            <p v-if="error" class="error" role="alert">{{ error }}</p>
            <button class="primary full" :disabled="pending">
              {{
                pending
                  ? tr("请稍候…", "Please wait…")
                  : session.setup
                    ? tr("创建并进入", "Create panel")
                    : tr("登录", "Sign in")
              }}
            </button>
          </form>
        </div>
      </main>
      <template v-else>
        <template v-if="!settings">
          <nav class="group-nav">
            <button
              class="mobile-menu icon-button"
              @click="menu = !menu"
              :title="tr('分组菜单', 'Groups')"
            >
              <PhList :size="22" /></button
            ><button
              :class="{ active: active === 'all' }"
              @click="
                active = 'all';
                menu = false;
              "
            >
              {{ tr("全部网站", "All websites") }}</button
            ><button
              v-for="g in config.groups"
              :key="g.id"
              :class="{ active: active === g.id }"
              @click="
                active = g.id;
                menu = false;
              "
            >
              {{ g.title }}
            </button>
            <div class="nav-spacer"></div>
            <button
              v-if="session.authenticated"
              class="edit-button"
              :class="{ active: edit }"
              @click="edit = !edit"
            >
              <PhCheck v-if="edit" :size="16" /><PhPencilSimple
                v-else
                :size="16"
              />{{ edit ? tr("完成整理", "Done") : tr("整理", "Organize") }}
            </button>
          </nav>
          <div v-if="menu" class="mobile-backdrop" @click="menu = false"></div>
          <aside v-if="menu" class="mobile-groups">
            <button
              @click="
                active = 'all';
                menu = false;
              "
            >
              {{ tr("全部网站", "All websites") }}</button
            ><button
              v-for="g in config.groups"
              :key="g.id"
              @click="
                active = g.id;
                menu = false;
              "
            >
              {{ g.title }}
            </button>
          </aside>
          <main class="board">
            <div class="mobile-search search">
              <PhMagnifyingGlass :size="19" /><input
                v-model="query"
                :placeholder="tr('搜索网站', 'Search websites')"
              />
            </div>
            <p v-if="error" class="error" role="alert">{{ error }}</p>
            <p v-if="edit" class="edit-hint">
              {{
                tr(
                  "拖动卡片调整顺序或移到其他分组；手机长按拖动。",
                  "Drag cards to reorder or move between groups; long press on touch devices.",
                )
              }}
            </p>
            <section v-for="g in visible" :key="g.id" class="website-group">
              <div class="section-heading">
                <h2><PhFolderSimple :size="22" />{{ g.title }}</h2>
                <div v-if="edit" class="row">
                  <button
                    class="icon-button"
                    @click="newGroup(config.groups.find((x) => x.id === g.id))"
                    :title="tr('编辑分组', 'Edit group')"
                  >
                    <PhPencilSimple :size="17" /></button
                  ><button
                    class="icon-button"
                    @click="newSite(g.id)"
                    :title="tr('添加网站', 'Add website')"
                  >
                    <PhPlus :size="19" />
                  </button>
                </div>
              </div>
              <draggable
                :list="config.groups.find((x) => x.id === g.id)!.sites"
                item-key="id"
                group="websites"
                class="site-grid"
                :class="{ 'is-editing': edit }"
                :disabled="!canDrag"
                :animation="180"
                :delay="220"
                :delay-on-touch-only="true"
                :touch-start-threshold="6"
                :fallback-tolerance="6"
                filter=".site-actions"
                :prevent-on-filter="false"
                @end="saveGuard"
              >
                <template #item="{ element: s }"
                  ><article
                    v-show="g.sites.some((x) => x.id === s.id)"
                    class="site-card"
                    :title="s.description || s.title"
                  >
                    <a
                      :href="s.url"
                      target="_blank"
                      rel="noopener noreferrer"
                      :tabindex="edit ? -1 : 0"
                      @click="edit && $event.preventDefault()"
                      ><span class="site-icon"
                        ><img
                          v-if="s.icon && !failedIcons.has(s.icon)"
                          :src="iconURL(s.icon)"
                          :alt="s.title"
                          width="36"
                          height="36"
                          loading="lazy"
                          decoding="async"
                          @error="
                            failedIcons.add(s.icon)
                          " /><PhGlobeHemisphereWest
                          v-else
                          :size="34"
                          weight="duotone" /></span
                      ><span class="site-copy"
                        ><strong>{{ s.title }}</strong
                        ><small>{{ domain(s.url) }}</small></span
                      ><PhArrowSquareOut class="open-arrow" :size="16"
                    /></a>
                    <div v-if="edit" class="site-actions">
                      <button
                        @click="editSite(s, g.id)"
                        :title="tr('编辑网站', 'Edit website')"
                      >
                        <PhPencilSimple :size="15" /></button
                      ><button
                        @click="removeSite(s)"
                        :title="tr('移除网站', 'Remove website')"
                      >
                        <PhX :size="15" />
                      </button>
                    </div></article
                ></template>
              </draggable>
              <div v-if="!g.sites.length" class="empty-group">
                <span>{{
                  tr("这个分组还没有网站", "No websites in this group")
                }}</span
                ><button
                  v-if="session.authenticated"
                  class="text-button"
                  @click="newSite(g.id)"
                >
                  {{ tr("添加网站", "Add website") }}
                </button>
              </div>
            </section>
            <div v-if="!visible.length" class="center-state">
              <PhMagnifyingGlass :size="30" />
              <p>{{ tr("没有匹配的网站", "No matching websites") }}</p>
            </div>
            <button v-if="edit" class="add-group" @click="newGroup()">
              <PhPlus :size="18" />{{ tr("添加分组", "Add group") }}
            </button>
          </main>
          <footer>
            <span>Nest Panel</span><span>MIT License</span
            ><button
              v-if="session.authenticated"
              @click="logout"
              :title="tr('退出登录', 'Sign out')"
            >
              <PhSignOut :size="17" />
            </button>
          </footer>
        </template>
        <main v-else class="settings-shell">
          <nav class="settings-nav">
            <button
              v-for="n in nav"
              :key="n.id"
              :class="{ active: settings === n.id }"
              @click="changeSettings(n.id)"
            >
              <component :is="n.icon" :size="19" /><span>{{
                lang === "zh" ? n.zh : n.en
              }}</span>
            </button>
          </nav>
          <div class="settings-content">
            <h1>
              {{
                nav.find((n) => n.id === settings)?.[
                  lang === "zh" ? "zh" : "en"
                ]
              }}
            </h1>
            <p v-if="error" class="error" role="alert">{{ error }}</p>
            <template v-if="settings === 'sites'"
              ><div class="section-toolbar">
                <p>
                  {{
                    tr(
                      "管理网站，或导入已有的 Sun-Panel 配置。",
                      "Manage your websites or import an existing Sun-Panel configuration.",
                    )
                  }}
                </p>
                <button class="primary" @click="newSite()">
                  <PhPlus :size="18" />{{ tr("添加网站", "Add website") }}
                </button>
              </div>
              <div class="setting-card">
                <h2>{{ tr("导入网站", "Import websites") }}</h2>
                <p>
                  {{
                    tr(
                      "支持 Sun-Panel v1 JSON 和栖页配置。旧版 Sun-Panel 文件不含上传的图标，缺失项使用默认图标。",
                      "Supports Sun-Panel v1 JSON and Nest Panel configuration. Missing uploaded icons use the bundled fallback.",
                    )
                  }}
                </p>
                <label class="file-button"
                  ><PhUploadSimple :size="18" />{{
                    tr("选择配置文件", "Choose configuration")
                  }}<input
                    type="file"
                    accept=".json"
                    @change="selectImport"
                    :disabled="pending"
                /></label>
                <div v-if="preview" class="import-preview">
                  <strong>{{ fileName }}</strong>
                  <p>
                    {{ preview.groups.length }} {{ tr("个分组", "groups") }} ·
                    {{ preview.sites }} {{ tr("个网站", "websites") }} ·
                    {{ preview.images }} {{ tr("张图片", "images") }}
                  </p>
                  <p>{{ preview.groups.join(" · ") }}</p>
                  <small v-for="warning in preview.warnings" :key="warning">{{
                    warning
                  }}</small
                  ><label
                    >{{ tr("导入方式", "Import mode")
                    }}<select v-model="importMode">
                      <option value="merge">
                        {{
                          tr(
                            "合并并跳过重复网址",
                            "Merge and skip duplicate URLs",
                          )
                        }}
                      </option>
                      <option value="replace">
                        {{
                          tr(
                            "替换网站、分组和主题",
                            "Replace websites, groups and appearance",
                          )
                        }}
                      </option>
                    </select></label
                  ><button
                    class="primary"
                    @click="doImport"
                    :disabled="pending"
                  >
                    {{ tr("确认导入", "Import") }}
                  </button>
                </div>
              </div>
              <div class="setting-card">
                <h2>{{ tr("补全图标", "Fetch missing icons") }}</h2>
                <p>
                  {{
                    tr(
                      "只处理缺少图标的网站，按顺序请求，失败时保留默认图标。",
                      "Processes only websites without icons, sequentially. Unavailable sites keep the fallback.",
                    )
                  }}
                </p>
                <button
                  v-if="!autoFilling"
                  @click="fillIcons"
                  :disabled="pending"
                >
                  <PhArrowClockwise :size="17" />{{
                    tr("获取缺失图标", "Fetch missing icons")
                  }}</button
                ><button v-else @click="cancelled = true">
                  {{ tr("停止获取", "Stop") }} {{ progress }}
                </button>
              </div>
              <div v-for="g in config.groups" :key="g.id" class="manage-group">
                <h2>{{ g.title }}</h2>
                <div v-for="s in g.sites" :key="s.id" class="manage-row">
                  <span class="site-icon small-icon"
                    ><img
                      v-if="s.icon && !failedIcons.has(s.icon)"
                      :src="iconURL(s.icon)"
                      alt=""
                      loading="lazy"
                      @error="failedIcons.add(s.icon)" /><PhGlobeHemisphereWest
                      v-else
                      :size="24"
                  /></span>
                  <div class="grow">
                    <strong>{{ s.title }}</strong
                    ><small>{{ s.url }}</small>
                  </div>
                  <button
                    class="icon-button"
                    @click="editSite(s, g.id)"
                    :title="tr('编辑', 'Edit')"
                  >
                    <PhPencilSimple :size="18" /></button
                  ><button
                    class="icon-button danger"
                    @click="removeSite(s)"
                    :title="tr('移除', 'Remove')"
                  >
                    <PhTrash :size="18" />
                  </button>
                </div></div
            ></template>
            <template v-if="settings === 'groups'"
              ><div class="section-toolbar">
                <p>
                  {{
                    tr(
                      "拖动把手调整分组顺序。",
                      "Drag the handles to reorder groups.",
                    )
                  }}
                </p>
                <button class="primary" @click="newGroup()">
                  <PhPlus :size="18" />{{ tr("添加分组", "Add group") }}
                </button>
              </div>
              <draggable
                v-model="config.groups"
                item-key="id"
                handle=".drag-handle"
                :animation="180"
                @end="saveGuard"
                ><template #item="{ element: g }"
                  ><div class="manage-row">
                    <span class="drag-handle" tabindex="0"
                      ><PhDotsSixVertical :size="22" /></span
                    ><PhFolderSimple :size="21" /><strong class="grow">{{
                      g.title
                    }}</strong
                    ><small>{{ g.sites.length }}</small
                    ><button
                      class="icon-button"
                      @click="newGroup(g)"
                      :title="tr('编辑', 'Edit')"
                    >
                      <PhPencilSimple :size="18" /></button
                    ><button
                      class="icon-button danger"
                      @click="removeGroup(g)"
                      :title="tr('移除', 'Remove')"
                    >
                      <PhTrash :size="18" />
                    </button></div></template></draggable
            ></template>
            <template v-if="settings === 'theme' && themeDraft"
              ><div class="setting-card">
                <h2>{{ tr("配色主题", "Color theme") }}</h2>
                <div class="theme-options">
                  <button
                    v-for="p in [
                      { id: 'graphite', zh: '石墨', en: 'Graphite' },
                      { id: 'ivory', zh: '暖白', en: 'Ivory' },
                      { id: 'ocean', zh: '深海', en: 'Ocean' },
                      { id: 'system', zh: '跟随系统', en: 'System' },
                    ]"
                    :key="p.id"
                    :class="{ selected: themeDraft.palette === p.id }"
                    @click="themeDraft.palette = p.id"
                  >
                    <span class="palette-swatch" :data-swatch="p.id"></span
                    >{{ lang === "zh" ? p.zh : p.en
                    }}<PhCheck v-if="themeDraft.palette === p.id" :size="17" />
                  </button>
                </div>
              </div>
              <div class="setting-card">
                <h2>{{ tr("网站布局", "Website layout") }}</h2>
                <div class="choice-grid">
                  <button
                    :class="{ selected: themeDraft.layout === 'cards' }"
                    @click="themeDraft.layout = 'cards'"
                  >
                    <PhCards :size="32" /><strong>{{
                      tr("横向卡片", "Horizontal cards")
                    }}</strong
                    ><small>{{
                      tr(
                        "图标、名称和域名横向排列",
                        "Icon, title and domain in a row",
                      )
                    }}</small></button
                  ><button
                    :class="{ selected: themeDraft.layout === 'grid' }"
                    @click="themeDraft.layout = 'grid'"
                  >
                    <PhSquaresFour :size="32" /><strong>{{
                      tr("图标网格", "Icon grid")
                    }}</strong
                    ><small>{{
                      tr(
                        "图标居上，名称和域名居下",
                        "Icon above title and domain",
                      )
                    }}</small>
                  </button>
                </div>
                <h3>{{ tr("图标样式", "Icon style") }}</h3>
                <div class="segmented">
                  <button
                    :class="{ selected: themeDraft.icons === 'original' }"
                    @click="themeDraft.icons = 'original'"
                  >
                    {{ tr("原始图标", "Original") }}</button
                  ><button
                    :class="{ selected: themeDraft.icons === 'tile' }"
                    @click="themeDraft.icons = 'tile'"
                  >
                    {{ tr("圆角底板", "Rounded tile") }}
                  </button>
                </div>
                <h3>{{ tr("排列密度", "Density") }}</h3>
                <div class="segmented">
                  <button
                    v-for="d in [
                      { id: 'compact', zh: '紧凑', en: 'Compact' },
                      { id: 'standard', zh: '标准', en: 'Standard' },
                      { id: 'relaxed', zh: '宽松', en: 'Relaxed' },
                    ]"
                    :key="d.id"
                    :class="{ selected: themeDraft.density === d.id }"
                    @click="themeDraft.density = d.id"
                  >
                    {{ lang === "zh" ? d.zh : d.en }}
                  </button>
                </div>
                <div
                  class="appearance-preview"
                  :data-layout="themeDraft.layout"
                >
                  <div class="site-card">
                    <a
                      ><span class="site-icon"
                        ><PhGlobeHemisphereWest
                          :size="34"
                          weight="duotone" /></span
                      ><span class="site-copy"
                        ><strong>{{ tr("示例网站", "Example website") }}</strong
                        ><small>example.com</small></span
                      ></a
                    >
                  </div>
                  <div class="site-card">
                    <a
                      ><span class="site-icon"
                        ><PhHouseLine :size="34" weight="duotone" /></span
                      ><span class="site-copy"
                        ><strong>{{ tr("我的服务", "My service") }}</strong
                        ><small>home.example.com</small></span
                      ></a
                    >
                  </div>
                </div>
              </div>
              <div class="setting-card">
                <h2>{{ tr("背景图片", "Background image") }}</h2>
                <p>
                  {{
                    tr(
                      "图片在本机服务保存，最大8MB，自动缩小至最长边1920px并移除元数据。",
                      "Stored on your server. Up to 8 MB, resized to a 1920px longest edge and stripped of metadata.",
                    )
                  }}
                </p>
                <div class="row wrap">
                  <label class="file-button"
                    ><PhImage :size="18" />{{
                      tr("上传背景", "Upload background")
                    }}<input
                      type="file"
                      accept="image/png,image/jpeg,image/webp,image/gif"
                      @change="upload($event, true)"
                      :disabled="pending" /></label
                  ><button
                    v-if="themeDraft.background"
                    @click="themeDraft.background = ''"
                  >
                    {{ tr("移除背景", "Remove background") }}
                  </button>
                </div>
                <h3>{{ tr("图片 API 地址", "Image API URL") }}</h3>
                <p>
                  {{
                    tr(
                      "支持图床随机图等直接返回图片或跳转到图片的接口。点击后保存来源并缓存当前图片，不自动轮询；原始地址不会发送给访客。",
                      "Supports endpoints returning or redirecting to an image. Fetch saves the source and caches the image without polling. The original URL is never sent to visitors.",
                    )
                  }}
                </p>
                <label
                  >{{ tr("背景接口", "Background API")
                  }}<input
                    v-model="backgroundURL"
                    type="url"
                    :placeholder="
                      backgroundStatus.configured
                        ? tr(
                            '来源已保存，留空使用原地址',
                            'Source saved; leave blank to keep it',
                          )
                        : 'https://gallery.example.com/api/random'
                    "
                    autocomplete="off" /></label
                ><small v-if="backgroundStatus.configured"
                  >{{ tr("已保存来源", "Saved source") }}:
                  {{ backgroundStatus.host }}</small
                >
                <div class="row wrap api-background-actions">
                  <button
                    @click="applyBackground()"
                    :disabled="
                      pending ||
                      (!backgroundURL && !backgroundStatus.configured)
                    "
                  >
                    <PhArrowClockwise :size="17" />{{
                      backgroundURL
                        ? tr("获取并应用", "Fetch and apply")
                        : tr("刷新背景", "Refresh background")
                    }}</button
                  ><button
                    v-if="backgroundStatus.configured"
                    class="text-button"
                    @click="applyBackground(true)"
                    :disabled="pending"
                  >
                    {{ tr("移除API来源", "Remove API source") }}
                  </button>
                </div>
                <label class="range-label"
                  >{{ tr("卡片不透明度", "Card opacity")
                  }}<output>{{ themeDraft.opacity }}%</output
                  ><input
                    type="range"
                    v-model.number="themeDraft.opacity"
                    min="25"
                    max="100" /></label
                ><label class="range-label"
                  >{{ tr("背景模糊", "Background blur")
                  }}<output>{{ themeDraft.blur }}px</output
                  ><input
                    type="range"
                    v-model.number="themeDraft.blur"
                    min="0"
                    max="24"
                /></label>
              </div>
              <div class="sticky-save">
                <span>{{
                  tr(
                    "预览效果不会自动保存",
                    "Preview changes are not saved automatically",
                  )
                }}</span
                ><button
                  class="primary"
                  :disabled="pending"
                  @click="
                    run(async () => {
                      const next = clone(config!);
                      next.theme = clone(themeDraft!);
                      await save(next);
                    })
                  "
                >
                  {{ tr("保存主题", "Save appearance") }}
                </button>
              </div></template
            >
            <template v-if="settings === 'security'"
              ><form class="setting-card" @submit.prevent="saveGuard">
                <h2>{{ tr("访问方式", "Access") }}</h2>
                <label
                  >{{ tr("导航名称", "Panel name")
                  }}<input
                    v-model="config.name"
                    required
                    maxlength="100" /></label
                ><label class="checkbox-row"
                  ><input v-model="config.public" type="checkbox" /><span>{{
                    tr("允许公开访问", "Allow public access")
                  }}</span></label
                >
                <p>
                  {{
                    tr(
                      "开启后，未登录访客可以查看全部网站和分组。设置、备份和容器信息仍需管理员登录。",
                      "When enabled, visitors can view every website and group. Settings, backups and Docker discovery remain administrator-only.",
                    )
                  }}
                </p>
                <button class="primary" :disabled="pending">
                  {{ tr("保存设置", "Save settings") }}
                </button>
              </form>
              <form class="setting-card" @submit.prevent="changePassword">
                <h2>{{ tr("账户密码", "Account password") }}</h2>
                <label
                  >{{ tr("当前密码", "Current password")
                  }}<input
                    v-model="currentPassword"
                    type="password"
                    required
                    autocomplete="current-password"
                    maxlength="72" /></label
                ><label
                  >{{ tr("账户名称", "Username")
                  }}<input
                    v-model="username"
                    required
                    autocomplete="username"
                    maxlength="64" /></label
                ><label
                  >{{ tr("新密码", "New password")
                  }}<input
                    v-model="newPassword"
                    type="password"
                    required
                    minlength="12"
                    maxlength="72"
                    autocomplete="new-password"
                /></label>
                <p>
                  {{
                    tr(
                      "至少12位，含大小写字母、数字和符号。修改后旧密码和所有已登录会话立即失效。",
                      "At least 12 characters with uppercase, lowercase, number and symbol. Changing credentials immediately revokes all sessions.",
                    )
                  }}
                </p>
                <button class="primary" :disabled="pending">
                  {{ tr("更新凭据", "Update credentials") }}
                </button>
              </form></template
            >
            <template v-if="settings === 'docker'"
              ><div class="setting-card">
                <h2>{{ tr("发现本机容器", "Discover local containers") }}</h2>
                <p>
                  {{
                    tr(
                      "读取运行中容器的映射端口，不会启动、停止或修改容器。需挂载 Docker Socket。",
                      "Reads published ports of running containers. Does not start, stop or modify containers. Requires a Docker socket mount.",
                    )
                  }}
                </p>
                <label
                  >{{
                    tr(
                      "生成链接的主机地址",
                      "Hostname or IP for generated links",
                    )
                  }}<input
                    v-model="dockerHost"
                    placeholder="nas.example.com" /></label
                ><button
                  class="primary"
                  @click="findDocker"
                  :disabled="finding"
                >
                  <PhArrowClockwise :size="18" />{{
                    finding
                      ? tr("正在发现…", "Discovering…")
                      : tr("扫描容器", "Scan containers")
                  }}
                </button>
              </div>
              <div v-if="candidates.length" class="setting-card">
                <h2>{{ tr("选择要添加的网站", "Select websites to add") }}</h2>
                <p>
                  {{
                    tr(
                      "请检查地址和端口；多端口容器可能包含非网页服务。",
                      "Check addresses and ports; containers may publish non-web services.",
                    )
                  }}
                </p>
                <div v-for="c in candidates" :key="c.id" class="docker-row">
                  <input
                    type="checkbox"
                    v-model="selected"
                    :value="c.id"
                    :disabled="c.existing"
                    :aria-label="c.name"
                  />
                  <div class="grow">
                    <strong
                      >{{ c.name }}
                      <small v-if="c.existing">{{
                        tr("已添加", "Added")
                      }}</small></strong
                    ><input
                      v-model="c.url"
                      :disabled="c.existing"
                      :aria-label="tr('网站地址', 'Website URL')"
                    />
                  </div>
                </div>
                <label
                  >{{ tr("目标分组", "Target group")
                  }}<select v-model="dockerGroup">
                    <option value="">
                      {{ tr("新建 Docker 分组", "Create Docker group") }}
                    </option>
                    <option
                      v-for="g in config.groups"
                      :key="g.id"
                      :value="g.id"
                    >
                      {{ g.title }}
                    </option>
                  </select></label
                ><button
                  class="primary"
                  @click="addDocker"
                  :disabled="pending || !selected.length"
                >
                  {{ tr("添加所选网站", "Add selected websites") }} ({{
                    selected.length
                  }})
                </button>
              </div></template
            >
            <template v-if="settings === 'backup'"
              ><div class="setting-card">
                <h2>{{ tr("配置导出", "Configuration export") }}</h2>
                <p>
                  {{
                    tr(
                      "JSON 包含网站、分组、主题、Base64图标和背景，不包含账户密码。适合迁移或分享，请注意其中的网站地址。",
                      "JSON includes websites, groups, appearance, Base64 icons and background, but excludes account credentials. Review website addresses before sharing.",
                    )
                  }}
                </p>
                <button
                  @click="
                    run(() => download('export', 'nest-panel-config.json'))
                  "
                  :disabled="pending"
                >
                  <PhDownloadSimple :size="18" />{{
                    tr("导出配置", "Export configuration")
                  }}</button
                ><button class="text-button" @click="openSettings('sites')">
                  {{ tr("导入配置 →", "Import configuration →") }}
                </button>
              </div>
              <div class="setting-card">
                <h2>
                  {{ tr("完整备份与恢复", "Complete backup and restore") }}
                </h2>
                <p>
                  {{
                    tr(
                      "加密保存全部配置、图片及管理员账户。换设备部署后先创建临时账户，再导入备份，恢复后使用原账户登录。",
                      "Encrypted backup includes all configuration, images and administrator credentials. On a new deployment, create a temporary account, then restore and sign in with the original account.",
                    )
                  }}
                </p>
                <label
                  >{{ tr("备份加密密码", "Backup encryption password")
                  }}<input
                    v-model="backupPassword"
                    type="password"
                    minlength="12"
                    maxlength="128"
                    autocomplete="off"
                    :placeholder="
                      tr(
                        '至少12位，请妥善保管',
                        'At least 12 characters; keep it safe',
                      )
                    "
                /></label>
                <div class="row wrap">
                  <button
                    class="primary"
                    @click="
                      run(() =>
                        download('backup', 'nest-panel-backup.nestbackup', {
                          password: backupPassword,
                        }),
                      )
                    "
                    :disabled="pending || backupPassword.length < 12"
                  >
                    <PhDownloadSimple :size="18" />{{
                      tr("下载完整备份", "Download full backup")
                    }}</button
                  ><label class="file-button"
                    ><PhUploadSimple :size="18" />{{
                      tr("选择备份包", "Choose backup")
                    }}<input
                      type="file"
                      accept=".nestbackup,.json"
                      @change="selectBackup"
                      :disabled="pending"
                  /></label>
                </div>
                <div v-if="backupFile" class="restore-box">
                  <strong>{{ fileName }}</strong>
                  <p>
                    {{
                      tr(
                        "在上方填写此备份的加密密码。恢复前会再次确认，成功后所有会话失效。",
                        "Enter the backup password above. You will confirm before restoring; all sessions will be revoked.",
                      )
                    }}
                  </p>
                  <button
                    class="danger"
                    @click="restore"
                    :disabled="pending || backupPassword.length < 12"
                  >
                    {{ tr("恢复备份", "Restore backup") }}
                  </button>
                </div>
              </div></template
            >
            <template v-if="settings === 'about'"
              ><div class="setting-card about">
                <PhHouseLine :size="46" weight="duotone" />
                <h2>栖页 · Nest Panel</h2>
                <p>v{{ session.version }} · MIT License</p>
                <p>
                  {{
                    tr(
                      "一个安静、轻巧的个人导航面板。",
                      "A quiet, lightweight home for your websites.",
                    )
                  }}
                </p>
                <p>
                  {{
                    tr(
                      "单容器、SQLite、内嵌前端；空闲不轮询，图标按需加载。",
                      "Single container, SQLite, embedded frontend. No idle polling; icons load on demand.",
                    )
                  }}
                </p>
                <a
                  href="https://github.com/Stylle/nest-panel"
                  target="_blank"
                  rel="noopener noreferrer"
                  >GitHub <PhArrowSquareOut :size="16"
                /></a></div
            ></template>
          </div>
        </main>
      </template>
      <div v-if="modal" class="modal-backdrop" @click.self="() => {}">
        <section
          class="modal"
          role="dialog"
          aria-modal="true"
          :aria-label="
            modal === 'site'
              ? tr('网站详情', 'Website details')
              : modal === 'group'
                ? tr('分组详情', 'Group details')
                : tr('登录', 'Sign in')
          "
          ref="dialog"
        >
          <header>
            <h2>
              {{
                modal === "site"
                  ? tr("网站详情", "Website details")
                  : modal === "group"
                    ? tr("分组详情", "Group details")
                    : tr("管理员登录", "Administrator sign in")
              }}
            </h2>
            <button
              class="icon-button"
              @click="closeModal"
              :disabled="pending || fetching"
              :title="tr('关闭', 'Close')"
            >
              <PhX :size="21" />
            </button>
          </header>
          <form v-if="modal === 'site'" @submit.prevent="saveSite">
            <label
              >{{ tr("网站地址", "Website URL") }}
              <div class="input-action">
                <input
                  v-model="site.url"
                  type="url"
                  required
                  maxlength="2048"
                  placeholder="https://example.com"
                  @blur="!site.title && site.url && getMetadata()"
                /><button
                  type="button"
                  @click="getMetadata"
                  :disabled="fetching || !site.url || pending"
                >
                  {{
                    fetching
                      ? tr("获取中…", "Fetching…")
                      : tr("自动获取", "Fetch")
                  }}
                </button>
              </div></label
            ><label
              >{{ tr("网站标题", "Title")
              }}<input v-model="site.title" required maxlength="100" /></label
            ><label
              >{{ tr("所属分组", "Group")
              }}<select v-model="siteGroup" required>
                <option v-for="g in config?.groups" :key="g.id" :value="g.id">
                  {{ g.title }}
                </option>
              </select></label
            ><label
              >{{ tr("描述（可选）", "Description (optional)")
              }}<textarea
                v-model="site.description"
                maxlength="1000"
                rows="2"
              ></textarea>
            </label>
            <div class="row icon-upload">
              <span class="site-icon"
                ><img
                  v-if="site.icon && !failedIcons.has(site.icon)"
                  :src="iconURL(site.icon)"
                  @error="failedIcons.add(site.icon)"
                  alt="" /><PhGlobeHemisphereWest v-else :size="34" /></span
              ><label class="file-button"
                >{{ tr("上传图标", "Upload icon")
                }}<input
                  type="file"
                  accept="image/*"
                  @change="upload($event)"
                  :disabled="pending" /></label
              ><button
                v-if="site.icon && !failedIcons.has(site.icon)"
                type="button"
                class="text-button"
                @click="site.icon = ''"
              >
                {{ tr("使用默认", "Use default") }}
              </button>
            </div>
            <p v-if="error" class="error" role="alert">{{ error }}</p>
            <div class="modal-actions">
              <button
                type="button"
                @click="closeModal"
                :disabled="pending || fetching"
              >
                {{ tr("取消", "Cancel") }}</button
              ><button class="primary" :disabled="pending || fetching">
                {{ tr("保存网站", "Save website") }}
              </button>
            </div>
          </form>
          <form v-else-if="modal === 'group'" @submit.prevent="saveGroup">
            <label
              >{{ tr("分组名称", "Group name")
              }}<input v-model="groupTitle" required maxlength="50"
            /></label>
            <p v-if="error" class="error">{{ error }}</p>
            <div class="modal-actions">
              <button type="button" @click="closeModal" :disabled="pending">
                {{ tr("取消", "Cancel") }}</button
              ><button class="primary" :disabled="pending">
                {{ tr("保存分组", "Save group") }}
              </button>
            </div>
          </form>
          <form
            v-else
            @submit.prevent="
              run(async () => {
                await api('login', 'POST', { username, password });
                password = '';
                modal = '';
                await load();
              })
            "
          >
            <label
              >{{ tr("账户名称", "Username")
              }}<input
                v-model="username"
                autocomplete="username"
                required /></label
            ><label
              >{{ tr("密码", "Password")
              }}<input
                v-model="password"
                type="password"
                autocomplete="current-password"
                required
                maxlength="72"
            /></label>
            <p v-if="error" class="error">{{ error }}</p>
            <button class="primary full" :disabled="pending">
              {{ tr("登录", "Sign in") }}
            </button>
          </form>
        </section>
      </div>
      <div v-if="toast" class="toast" role="status">
        <PhCheck :size="18" />{{ toast }}
      </div>
    </div>
  </div>
</template>
