import Link from 'next/link';
import { KBPage } from '@/lib/api';

function TagBadge({ tag }: { tag: string }) {
  return (
    <span className="inline-block rounded-full bg-indigo-50 px-2 py-0.5 text-xs font-medium text-indigo-600 ring-1 ring-inset ring-indigo-200">
      {tag}
    </span>
  );
}

function CategoryBadge({ category }: { category: string }) {
  return (
    <span className="inline-block rounded bg-amber-50 px-2 py-0.5 text-xs font-semibold text-amber-700 ring-1 ring-inset ring-amber-200">
      {category}
    </span>
  );
}

interface PageCardProps {
  page: KBPage;
}

export default function PageCard({ page }: PageCardProps) {
  const updatedAt = new Date(page.updated_at).toLocaleDateString('ja-JP', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });

  return (
    <Link
      href={`/pages/${page.page_id}`}
      className="group flex flex-col gap-3 rounded-xl border border-slate-200 bg-white p-5 shadow-sm transition-all hover:shadow-md hover:border-indigo-300"
    >
      {/* Header row */}
      <div className="flex items-start justify-between gap-2">
        <h3 className="text-base font-semibold text-slate-900 group-hover:text-indigo-600 transition-colors leading-snug line-clamp-2">
          {page.title}
        </h3>
        {page.category && <CategoryBadge category={page.category} />}
      </div>

      {/* Summary */}
      {page.summary && (
        <p className="text-sm text-slate-500 line-clamp-3 leading-relaxed">
          {page.summary}
        </p>
      )}

      {/* Tags */}
      {page.tags && page.tags.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {page.tags.slice(0, 4).map((tag) => (
            <TagBadge key={tag} tag={tag} />
          ))}
          {page.tags.length > 4 && (
            <span className="text-xs text-slate-400">+{page.tags.length - 4}</span>
          )}
        </div>
      )}

      {/* Footer */}
      <div className="mt-auto flex items-center justify-between text-xs text-slate-400 pt-1 border-t border-slate-100">
        <span>更新: {updatedAt}</span>
        <span className="text-indigo-500 font-medium group-hover:underline">読む →</span>
      </div>
    </Link>
  );
}
