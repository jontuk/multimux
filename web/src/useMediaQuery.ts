import { useEffect, useState } from "react";

// Phones (either orientation) and tablets get the mobile session view; the
// grid needs a pointer and more room than they have. Width alone is not
// enough — a phone in landscape is ~840px wide — so a touch screen with no
// hover qualifies regardless of size. Mirrored by the matching @media block
// in index.css; change them together.
export const MOBILE_VIEW_QUERY = "(max-width: 560px), (pointer: coarse) and (hover: none)";

export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => window.matchMedia?.(query).matches ?? false);

  useEffect(() => {
    const media = window.matchMedia?.(query);
    if (!media) return;
    const update = () => setMatches(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, [query]);

  return matches;
}
