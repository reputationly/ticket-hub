import { createContext, useContext } from "react";
import type { Catalog, Settings, User } from "./api";
export interface HubContext {
  user: User;
  setUser: (u: User | null) => void;
  site: Settings;
  reloadSite: () => Promise<void>;
  catalog: Catalog;
  unread: number;
  refreshUnread: () => Promise<void>;
  revision: number;
  live: boolean;
}
export const Hub = createContext<HubContext | null>(null);
export function useHub() {
  const value = useContext(Hub);
  if (!value) throw new Error("尚未登录");
  return value;
}
