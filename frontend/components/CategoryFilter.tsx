'use client';

import { useRouter, useSearchParams } from 'next/navigation';

interface CategoryFilterProps {
  categories: string[];
  selected?: string;
}

export default function CategoryFilter({ categories, selected }: CategoryFilterProps) {
  const router = useRouter();
  const searchParams = useSearchParams();

  function handleSelect(cat: string | null) {
    const params = new URLSearchParams(searchParams.toString());
    if (cat) {
      params.set('category', cat);
    } else {
      params.delete('category');
    }
    router.push(`/pages?${params.toString()}`);
  }

  return (
    <div className="flex flex-wrap gap-2">
      <button
        onClick={() => handleSelect(null)}
        className={`rounded-full px-4 py-1.5 text-sm font-medium transition-colors ${
          !selected
            ? 'bg-indigo-600 text-white'
            : 'bg-white text-slate-600 border border-slate-200 hover:border-indigo-300 hover:text-indigo-600'
        }`}
      >
        すべて
      </button>
      {categories.map((cat) => (
        <button
          key={cat}
          onClick={() => handleSelect(cat)}
          className={`rounded-full px-4 py-1.5 text-sm font-medium transition-colors ${
            selected === cat
              ? 'bg-indigo-600 text-white'
              : 'bg-white text-slate-600 border border-slate-200 hover:border-indigo-300 hover:text-indigo-600'
          }`}
        >
          {cat}
        </button>
      ))}
    </div>
  );
}
