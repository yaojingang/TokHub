import { Link, useLocation } from "react-router-dom";
import { useEffect, useState } from "react";
import { currentUser, siteConfig } from "../lib/api";
import type { SiteConfig, User } from "../lib/api";
import { defaultPrimaryPublicLinks, normalizeLegacyPublicLinks } from "../lib/publicLinks";

type PublicNavProps = {
  onAuthClick?: () => void;
};

type BeforeInstallPromptEvent = Event & {
  platforms: string[];
  userChoice: Promise<{ outcome: "accepted" | "dismissed"; platform: string }>;
  prompt(): Promise<void>;
};

type StandaloneNavigator = Navigator & {
  standalone?: boolean;
};

function userInitial(user: User) {
  const source = user.avatar || user.name || user.username || user.email || "T";
  return source.slice(0, 1).toUpperCase();
}

function isStandaloneDisplay() {
  return window.matchMedia("(display-mode: standalone)").matches || Boolean((window.navigator as StandaloneNavigator).standalone);
}

export function PublicNav({ onAuthClick }: PublicNavProps = {}) {
  const location = useLocation();
  const [user, setUser] = useState<User | null>(null);
  const [site, setSite] = useState<SiteConfig | null>(null);
  const [installPrompt, setInstallPrompt] = useState<BeforeInstallPromptEvent | null>(null);
  const [installNotice, setInstallNotice] = useState("");
  const [standalone, setStandalone] = useState(false);

  useEffect(() => {
    let active = true;
    const handleCurrentUserChanged = (event: Event) => {
      setUser((event as CustomEvent<User | null>).detail ?? null);
    };

    window.addEventListener("tokhub:current-user-changed", handleCurrentUserChanged);
    currentUser({ force: true })
      .then((value) => {
        if (active) setUser(value);
      })
      .catch(() => {
        if (active) setUser(null);
      });
    siteConfig().then(setSite).catch(() => setSite(null));
    return () => {
      active = false;
      window.removeEventListener("tokhub:current-user-changed", handleCurrentUserChanged);
    };
  }, []);

  useEffect(() => {
    const displayMode = window.matchMedia("(display-mode: standalone)");
    setStandalone(isStandaloneDisplay());

    function handleDisplayModeChange() {
      setStandalone(isStandaloneDisplay());
    }

    function handleBeforeInstallPrompt(event: Event) {
      event.preventDefault();
      setInstallNotice("");
      setInstallPrompt(event as BeforeInstallPromptEvent);
    }

    function handleAppInstalled() {
      setInstallPrompt(null);
      setInstallNotice("");
      setStandalone(true);
    }

    displayMode.addEventListener("change", handleDisplayModeChange);
    window.addEventListener("beforeinstallprompt", handleBeforeInstallPrompt);
    window.addEventListener("appinstalled", handleAppInstalled);
    return () => {
      displayMode.removeEventListener("change", handleDisplayModeChange);
      window.removeEventListener("beforeinstallprompt", handleBeforeInstallPrompt);
      window.removeEventListener("appinstalled", handleAppInstalled);
    };
  }, []);

  async function installWorkbench() {
    if (standalone) {
      setInstallNotice("当前已经是独立应用窗口。");
      return;
    }
    if (!installPrompt) {
      setInstallNotice("如果没有弹出安装框，请在 Chrome 地址栏右侧或右上角菜单中选择安装页面；Safari 可从文件菜单选择添加到 Dock。");
      return;
    }
    const prompt = installPrompt;
    setInstallPrompt(null);
    await prompt.prompt();
    const choice = await prompt.userChoice.catch(() => undefined);
    if (choice?.outcome === "dismissed") {
      setInstallNotice("Chrome 暂时不会再次自动提示。仍可从地址栏右侧或右上角菜单手动安装。");
    }
  }

  const links = normalizeLegacyPublicLinks(site?.navItems?.length ? site.navItems : defaultPrimaryPublicLinks);
  const canRegister = site ? site.registrationOpen && site.showRegisterCta : true;
  const authLabel = canRegister ? "登录 / 注册" : "登录";

  return (
    <nav className="nav">
      <div className="nav-inner">
        <Link className="logo" to="/">
          <span className="logo-mark">{site?.logoMark || "T"}</span>
          <span className="logo-text">
            <b>{site?.brandName || "TokHub"}</b>
            <small>{site ? site.subtitle : "API 中转站监控"}</small>
          </span>
        </Link>
        <div className="nav-links">
          {links.map(({ href, label }) => (
            <a className={location.pathname === href ? "active" : ""} href={href} key={href}>
              {label}
            </a>
          ))}
        </div>
        <div className="nav-right">
          <span className="live-dot">
            <i /> 实时监控中
          </span>
          <div className="nav-install-wrap">
            <button className="btn btn-ghost btn-sm nav-install-btn" type="button" onClick={() => void installWorkbench()}>
              安装工作台
            </button>
            {installNotice ? <div className="nav-install-note">{installNotice}</div> : null}
          </div>
          {user ? (
            <>
              <a className="btn btn-ghost btn-sm" href="/console">
                控制台
              </a>
              <a className="avatar" href="/console" title="账户">
                {userInitial(user)}
              </a>
            </>
          ) : onAuthClick ? (
            <button className="btn btn-ghost btn-sm public-auth-trigger" type="button" onClick={onAuthClick}>
              {authLabel}
            </button>
          ) : (
            <a className="btn btn-ghost btn-sm public-auth-trigger" href="/login">
              {authLabel}
            </a>
          )}
        </div>
      </div>
    </nav>
  );
}
