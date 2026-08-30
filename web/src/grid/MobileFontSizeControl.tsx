import { createPortal } from "react-dom";
import { MOBILE_FONT_SIZES, type MobileFontSize } from "./mobileFontSize";

export default function MobileFontSizeControl({
  controlsSlot,
  value,
  onChange,
}: {
  controlsSlot: HTMLElement | null;
  value: MobileFontSize;
  onChange: (size: MobileFontSize) => void;
}) {
  if (!controlsSlot) return null;
  return createPortal(
    <select
      className="mobile-font-size-select"
      aria-label="Terminal font size"
      value={value}
      onChange={(event) => onChange(Number(event.target.value) as MobileFontSize)}
    >
      {MOBILE_FONT_SIZES.map((size) => (
        <option key={size} value={size}>
          {size} px
        </option>
      ))}
    </select>,
    controlsSlot,
  );
}
