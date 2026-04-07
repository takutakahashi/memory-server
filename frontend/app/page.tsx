import Link from 'next/link';
import { listPages } from '@/lib/api';
import PageCard from '@/components/PageCard';

async function getRecentPages() {
  try {
    const result = await listPages({ limit: 6 });
    return result.pages || [];
  } catch {
    return [];
  }
}

async function getCategories() {
  try {
    const result = await listPages({ limit: 100 });
    const pages = result.pages || [];
    const cats = [...new Set(pages.map((p) => p.category).filter(Boolean))];
    return cats;
  } catch {
    return [];
  }
}

export default async function HomePage() {
  const [recentPages, categories] = await Promise.all([
    getRecentPages(),
    getCategories(),
  ]);

  return (
    <div className="mx-auto max-w-7xl px-4 sm:px-6 py-10 space-y-12">
      {/* Hero */}
      <section className="rounded-2xl bg-gradient-to-br from-indigo-600 to-violet-600 p-8 sm:p-12 text-white">
        <h1 className="text-3xl sm:text-4xl font-bold mb-3">社内 Wiki へようこそ</h1>
        <p className="text-indigo-100 max-w-2xl mb-6 leading-relaxed">
          チームの知識を一か所に集約した社内ポータルです。記事を検索したり、カテゴリから探したりできます。
        </p>
        <div className="flex flex-wrap gap-3">
          <Link
            href="/pages"
            className="inline-flex items-center gap-2 rounded-lg bg-white px-5 py-2.5 font-semibold text-indigo-600 hover:bg-indigo-50 transition-colors"
          >
            記事一覧を見る
            <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
            </svg>
          </Link>
          <Link
            href="/search"
            className="inline-flex items-center gap-2 rounded-lg border border-white/30 bg-white/10 px-5 py-2.5 font-semibold text-white hover:bg-white/20 transition-colors"
          >
            検索する
          </Link>
        </div>
      </section>

      {/* Stats */}
      <section className="grid grid-cols-2 sm:grid-cols-3 gap-4">
        <div className="rounded-xl border border-slate-200 bg-white p-5 text-center shadow-sm">
          <div className="text-3xl font-bold text-indigo-600">{recentPages.length}+</div>
          <div className="text-sm text-slate-500 mt-1">記事</div>
        </div>
        <div className="rounded-xl border border-slate-200 bg-white p-5 text-center shadow-sm">
          <div className="text-3xl font-bold text-indigo-600">{categories.length}</div>
          <div className="text-sm text-slate-500 mt-1">カテゴリ</div>
        </div>
        <div className="rounded-xl border border-slate-200 bg-white p-5 text-center shadow-sm col-span-2 sm:col-span-1">
          <div className="text-3xl font-bold text-indigo-600">∞</div>
          <div className="text-sm text-slate-500 mt-1">知識</div>
        </div>
      </section>

      {/* Categories */}
      {categories.length > 0 && (
        <section>
          <h2 className="text-xl font-bold text-slate-800 mb-4">カテゴリ</h2>
          <div className="flex flex-wrap gap-2">
            {categories.map((cat) => (
              <Link
                key={cat}
                href={`/pages?category=${encodeURIComponent(cat)}`}
                className="rounded-full border border-slate-200 bg-white px-4 py-2 text-sm font-medium text-slate-700 shadow-sm hover:border-indigo-300 hover:text-indigo-600 transition-colors"
              >
                {cat}
              </Link>
            ))}
          </div>
        </section>
      )}

      {/* Recent pages */}
      <section>
        <div className="flex items-center justify-between mb-5">
          <h2 className="text-xl font-bold text-slate-800">最近の記事</h2>
          <Link href="/pages" className="text-sm font-medium text-indigo-600 hover:underline">
            すべて見る →
          </Link>
        </div>
        {recentPages.length === 0 ? (
          <div className="rounded-xl border border-dashed border-slate-300 bg-white p-12 text-center text-slate-400">
            <svg xmlns="http://www.w3.org/2000/svg" className="mx-auto h-12 w-12 mb-3 text-slate-300" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
            </svg>
            <p className="font-medium">まだ記事がありません</p>
            <p className="text-sm mt-1">Knowledge Base に記事を追加してみましょう</p>
          </div>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {recentPages.map((page) => (
              <PageCard key={page.page_id} page={page} />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
