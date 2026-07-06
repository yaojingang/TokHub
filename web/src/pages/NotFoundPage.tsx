import { Footer } from "../components/Footer";
import { PublicNav } from "../components/PublicNav";

export function NotFoundPage() {
  return (
    <div className="app">
      <PublicNav />
      <main className="page not-found-page">
        <section className="section">
          <div className="section-head">
            <div>
              <h1>页面不存在</h1>
              <p>这个地址没有对应的公开页面或后台模块。你可以返回前台首页，查看监控总览，或进入自己的控制台继续管理通道和网关。</p>
            </div>
            <div className="hero-actions">
              <a className="btn btn-primary btn-sm" href="/">
                返回首页
              </a>
              <a className="btn btn-ghost btn-sm" href="/dashboard">
                监控总览
              </a>
              <a className="btn btn-ghost btn-sm" href="/console">
                进入控制台
              </a>
            </div>
          </div>
        </section>
      </main>
      <Footer />
    </div>
  );
}
