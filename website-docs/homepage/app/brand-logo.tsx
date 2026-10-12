import Image from "next/image";
import { homeAssets } from "../../shared/header";

/** Horizontal Gamelib wordmark for the product homepage masthead. */
export function BrandLogo({ priority = false }: { priority?: boolean }) {
  return (
    <span className="wk-logo">
      <Image
        src={`${homeAssets}/brand/gamelib.png`}
        alt="Gamelib"
        width={1150}
        height={256}
        priority={priority}
      />
    </span>
  );
}
