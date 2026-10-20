import { api } from "../api";
import type { Account, Device, Grant, Items, Resource, Task, SourceUsage, ClientCatalog } from "./models";
export const client = {
  catalog: (admin=false) => api(admin ? "/admin/clients" : "/clients") as Promise<ClientCatalog>,
  sourceUsage: () => api("/admin/sources/usage") as Promise<SourceUsage>,
  users: (query = "") => api("/admin/users" + query) as Promise<Items<Account>>,
  devices: () => api("/admin/devices") as Promise<Items<Device>>,
  grants: () => api("/admin/grants") as Promise<Items<Grant>>,
  tasks: (query = "") => api("/admin/tasks" + query) as Promise<Items<Task>>,
  resources: async (kind: string): Promise<Items<Resource>> => {
    const first = (await api(
      "/admin/resources/" + kind + "?page=1&page_size=500",
    )) as Items<Resource>;
    if ((first.total ?? first.items.length) > 50000)
      throw Error("库存超过当前选择器加载上限，请缩小范围");
    const items = [...first.items];
    for (let page = 2; items.length < (first.total ?? 0); page++) {
      const next = (await api(
        "/admin/resources/" + kind + "?page=" + page + "&page_size=500",
      )) as Items<Resource>;
      if (!next.items.length) throw Error("资源列表正在变化，请刷新后重试");
      items.push(...next.items);
    }
    return { ...first, items };
  },
};
