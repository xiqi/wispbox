import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { get } from "./api";

export type Brand = {
  name: string;
  logo?: string;
};

type BrandContextValue = Brand & {
  setBrand: (brand: Brand) => void;
  reloadBrand: () => Promise<void>;
};

const defaultBrand: Brand = { name: "wispbox", logo: "" };
const domainBrandPrefix = "brand_domain:";

const BrandContext = createContext<BrandContextValue>({
  ...defaultBrand,
  setBrand: () => {},
  reloadBrand: async () => {},
});

function cleanBrand(brand: Partial<Brand> | undefined): Brand {
  const name = brand?.name?.trim() || defaultBrand.name;
  return { name, logo: brand?.logo || "" };
}

export function BrandProvider({ children }: { children: ReactNode }) {
  const [brand, setBrandState] = useState<Brand>(defaultBrand);

  const setBrand = useCallback((next: Brand) => {
    setBrandState(cleanBrand(next));
  }, []);

  const reloadBrand = useCallback(async () => {
    const res = await get<{ brand: Brand }>("/api/brand");
    setBrand(res.brand);
  }, [setBrand]);

  useEffect(() => {
    reloadBrand().catch(() => {
      /* keep defaults when the API is unavailable */
    });
  }, [reloadBrand]);

  const value = useMemo(
    () => ({ ...brand, setBrand, reloadBrand }),
    [brand, setBrand, reloadBrand],
  );

  return <BrandContext.Provider value={value}>{children}</BrandContext.Provider>;
}

export function useBrand() {
  return useContext(BrandContext);
}

export function brandSettingKeys(domain = "") {
  const cleanDomain = domain.trim().toLowerCase();
  if (!cleanDomain) {
    return { name: "brand_name", logo: "brand_logo" };
  }
  return {
    name: `${domainBrandPrefix}${cleanDomain}:brand_name`,
    logo: `${domainBrandPrefix}${cleanDomain}:brand_logo`,
  };
}

export function brandFromSettings(settings: Record<string, string>, domain = ""): Brand {
  const keys = brandSettingKeys(domain);
  const globalKeys = brandSettingKeys();
  return cleanBrand({
    name: settings[keys.name] || settings[globalKeys.name],
    logo: settings[keys.logo] || settings[globalKeys.logo],
  });
}
