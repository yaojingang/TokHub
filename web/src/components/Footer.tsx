import { useEffect, useState } from "react";
import { siteConfig, type SiteConfig } from "../lib/api";
import { defaultFooterPublicLinks, normalizeLegacyPublicLinks } from "../lib/publicLinks";

export function Footer({ admin = false, hidePlatformAdmin = !admin }: { admin?: boolean; hidePlatformAdmin?: boolean }) {
  const [site, setSite] = useState<SiteConfig | null>(null);

  useEffect(() => {
    siteConfig().then(setSite).catch(() => setSite(null));
  }, []);

  const links = normalizeLegacyPublicLinks(site?.footerLinks?.length ? site.footerLinks : defaultFooterPublicLinks)
    .filter((link) => !(hidePlatformAdmin && isPlatformAdminHref(link.href)));

  return (
    <footer className={admin ? "tk-footer tk-footer-admin" : "tk-footer"}>
      <div className="tkf-inner">
        <div className="tkf-brand">
          <span className="logo-mark">{site?.logoMark || "T"}</span>
          <span className="tkf-meta">
            <b>{site?.brandName || "TokHub"}</b>
            <small>{site ? site.subtitle : "API 中转站监控与企业网关"}</small>
          </span>
        </div>
        <div className="tkf-links">
          {links.map((link) => (
            <a href={link.href} key={`${link.href}-${link.label}`}>
              {link.label}
            </a>
          ))}
        </div>
        <div className="tkf-right">
          <div className="tkf-copyright" aria-label="TokHub copyright">
            TokHub API 中转站监控与企业网关 © 2026{" "}
            <a href="https://www.tokhub.me/" target="_blank" rel="noreferrer">Tokhub</a>
          </div>
        </div>
      </div>
    </footer>
  );
}

function isPlatformAdminHref(href: string) {
  const value = href.trim();
  return value === "/admin" || value.startsWith("/admin/") || value.startsWith("/admin?") || value.startsWith("/admin#");
}
