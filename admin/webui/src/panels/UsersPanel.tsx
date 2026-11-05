import { useEffect, useRef, useState } from "react";
import { useApp } from "../ctx";
import { fmtShort } from "../lib/dates";

export function UsersPanel({ selected, onSelect }: { selected: string | null; onSelect: (emp: string | null) => void }) {
  const { users, openModal } = useApp();
  const [q, setQ] = useState("");
  const [menu, setMenu] = useState<string | null>(null);
  const wrap = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!menu) return;
    const close = (e: MouseEvent) => { if (!wrap.current?.contains(e.target as Node)) setMenu(null); };
    const esc = (e: KeyboardEvent) => { if (e.key === "Escape") setMenu(null); };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", esc);
    return () => { document.removeEventListener("mousedown", close); document.removeEventListener("keydown", esc); };
  }, [menu]);

  const needle = q.trim().toLowerCase();
  const list = needle ? users.filter((u) => u.name.toLowerCase().includes(needle) || u.employee_id.toLowerCase().includes(needle)) : users;

  return (
    <section className="panel users" aria-label="Users">
      <div className="panel-head">
        <h2>Users</h2>
        <span className="muted">{users.length}</span>
        <input className="search" type="search" placeholder="Search name or id" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search users" />
      </div>
      <div className="panel-body scroll" ref={wrap}>
        {users.length === 0 && <p className="muted">No faculty yet. Add one from the Server status panel.</p>}
        {users.length > 0 && list.length === 0 && <p className="muted">No user matches “{q}”.</p>}
        <ul className="rows">
          {list.map((u) => {
            const sel = selected === u.employee_id;
            return (
              <li key={u.employee_id} className={sel ? "user-row selected" : "user-row"}>
                <button className="user-main" aria-pressed={sel} onClick={() => onSelect(sel ? null : u.employee_id)}>
                  <span className={u.enrolled ? "dot on" : "dot"} aria-label={u.enrolled ? "Enrolled" : "Not enrolled"} />
                  <span className="u-name">{u.name}<small>{u.employee_id}</small></span>
                  <span className="u-stat">{u.days_present} days<small>{u.last_marked_day ? `last ${fmtShort(u.last_marked_day)}` : "never"}</small></span>
                </button>
                <button className="icon-btn kebab" aria-haspopup="menu" aria-expanded={menu === u.employee_id} aria-label={`Actions for ${u.name}`}
                  onClick={() => setMenu(menu === u.employee_id ? null : u.employee_id)}>⋯</button>
                {menu === u.employee_id && (
                  <div className="menu" role="menu">
                    <button role="menuitem" onClick={() => { setMenu(null); openModal({ kind: "code", employee_id: u.employee_id }); }}>Enrollment code</button>
                    <button role="menuitem" onClick={() => { setMenu(null); openModal({ kind: "mark", employee_id: u.employee_id }); }}>Mark manually</button>
                    <button role="menuitem" className="danger-text" disabled={!u.enrolled} onClick={() => { setMenu(null); openModal({ kind: "revoke", employee_id: u.employee_id }); }}>Revoke phone key</button>
                    <button role="menuitem" className="danger-text" onClick={() => { setMenu(null); openModal({ kind: "delete", employee_id: u.employee_id }); }}>Delete faculty</button>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      </div>
    </section>
  );
}
