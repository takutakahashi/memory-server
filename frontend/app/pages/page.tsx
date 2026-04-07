import { listPages, KBPage } from '@/lib/api';
import PageCard from '@/components/PageCard';
import CategoryFilter from '@/components/CategoryFilter';
import { Suspense } from 'react';

interface PageListProps {
  searchParams: Promise<{ category?: string; next_token?: string }>;
}

async function PageListContent({ category, nextToken }: { category?: string; nextToken?: string }) {
  let pages: KBPage[] = [];
  let nextNextToken: string | undefined;
  let error: string | null = null;

  try {
    const result = await listPages({
      category: category || undefined,
      limit: 24,
      next_token: nextToken,
    });
    pages = result.pages || [];
    nextNextToken = result.next_token;
  } catch (e) {
    error = e instanceof Error ? e.message : '記事の取得に失敗しました';
  }

  if (error) {
    return (
      <div className="rounded-xl border border-red-200 bg-red-50 p-6 text-red-700">
        <p className="font-medium">エラーが発生しました</p>
        <p className="text-sm mt-1">{error}</p>
      </div>
    );
  }

  if (pages.length === 0) {
    return (
      <div className="rounded-xl border border-dashed border-slate-300 bg-white p-12 text-center text-slate-400">
        <svg xmlns="http://www.w3.org/2000/svg" className="mx-auto h-12 w-12 mb-3 text-slate-300" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
        </svg>
        <p className="font-medium">
          {category ? `「${category}」の記事が見つかりませんでした` : 'まだ記事がありません'}
        </p>
      </div>
    );
  }

  return (
    <>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {pages.map((page) => (
          <PageCard key={page.page_id} page={page} />
        ))}
      </div>
      {nextNextToken && (
        <div className="mt-8 text-center">
          <a
            href={`/pages?${category ? `category=${encodeURIComponent(category)}&` : ''}next_token=${encodeURIComponent(nextNextToken)}`}
            className="inline-flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-6 py-2.5 text-sm font-medium text-slate-700 shadow-sm hover:border-indigo-300 hover:text-indigo-600 transition-colors"
          >
            もっと見る
          </a>
        </div>
      )}
    </>
  );
}

async function CategoriesLoader() {
  try {
    const result = await listPages({ limit: 200 });
    const pages = result.pages || [];
    return [...new Set(pages.map((p) => p.category).filter(Boolean))];
  } catch {
    return [];
  }
}

export default async function PagesListPage({ searchParams }: PageListProps) {
  const { category, next_token } = await searchParams;
  const categories = await CategoriesLoader();

  return (
    <div className="mx-auto max-w-7xl px-4 sm:px-6 py-10 space-y-8">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-slate-900">記事一覧</h1>
        <p className="text-slate-500 mt-1 text-sm">
          {category ? `カテゴリ「${category}」の記事` : 'すべての記事'}
        </p>
      </div>

      {/* Category filter */}
      {categories.length > 0 && (
        <Suspense fallback={null}>
          <CategoryFilter categories={categories} selected={category} />
        </Suspense>
      )}

      {/* Page list */}
      <Suspense
        fallback={
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {[...Array(6)].map((_, i) => (
              <div key={i} className="h-40 rounded-xl border border-slate-200 bg-white animate-pulse" />
            ))}
          </div>
        }
      >
        <PageListContent category={category} nextToken={next_token} />
      </Suspense>
    </div>
  );
}
