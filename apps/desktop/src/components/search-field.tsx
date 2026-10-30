import { SearchIcon } from "#/components/icons/search.tsx";

/**
 * The search field at the top of a list (DESIGN.md §6 "Inputs"): a pill with the magnifier at
 * the left. It filters as the user types.
 */
export const SearchField = ({
  label,
  placeholder,
  value,
  onChange,
}: {
  /** The accessible name; the placeholder is what shows. */
  label: string;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
}) => (
  <div className="text-input-placeholder relative flex items-center">
    <span className="pointer-events-none absolute left-3 flex">
      <SearchIcon size="xs" />
    </span>
    <input
      type="search"
      aria-label={label}
      placeholder={placeholder}
      value={value}
      spellCheck={false}
      autoComplete="off"
      onChange={(e) => {
        onChange(e.target.value);
      }}
      className="rounded-input-search border-input-border bg-input-bg type-secondary text-input-text focus:border-input-border-focus h-input-search pr-input-search-right pl-input-search-left w-full border py-0 outline-none focus-visible:outline-none"
    />
  </div>
);
