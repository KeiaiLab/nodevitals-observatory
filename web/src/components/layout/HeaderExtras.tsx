// HeaderExtras — 상단 헤더 부품 모음(전역 검색·실시간 시계·알림·도움말·사용자).
//
// 각 부품은 서로를 모른다. 배치는 Shell 이 한다 — 헤더는 화면 폭에 따라 구성이
// 바뀌는 자리라서, 부품을 한 덩어리로 묶어두면 배치를 못 바꾼다.
import { useEffect, useState, useMemo } from 'react';
import { Bell, CircleHelp, Search } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { NAV_TOP, visibleGroups } from '@/lib/navigation';

/** 검색 후보 = 좌측 메뉴에 실제로 보이는 화면만. 미구현 항목을 노출하면
 *  "검색은 되는데 안 열린다"가 되고, 데모에서 감춘 화면을 노출하면 메뉴에서
 *  치운 것을 검색이 도로 꺼내 준다 — 두 목록은 같은 필터를 써야 한다.
 *  navigation.ts 가 SSOT. */
function searchItems(demo: boolean) {
  return NAV_TOP.flatMap((top) =>
    visibleGroups(top, demo).flatMap((group) =>
      group.items.map((leaf) => ({
        label: leaf.label,
        to: leaf.to as string,
        section: top.label,
      })),
    ),
  );
}

/** 전역 검색 — Ctrl/Cmd + K 커맨드 팔레트. */
export function GlobalSearch({
  onNavigate,
  demo = false,
}: {
  onNavigate?: (to: string) => void;
  demo?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const items = useMemo(() => searchItems(demo), [demo]);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== 'k' || !(e.metaKey || e.ctrlKey)) return;
      // 입력 중인 필드에서는 브라우저·앱 단축키를 가로채지 않는다.
      const el = document.activeElement;
      if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) return;
      e.preventDefault();
      setOpen((v) => !v);
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label="전역 검색"
        className="flex h-9 items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--card)] px-3 text-sm text-[var(--muted-foreground)] transition-colors hover:bg-accent md:w-96"
      >
        <Search className="size-4 shrink-0" />
        <span className="hidden truncate md:inline">
          클러스터, 노드, GPU, 테넌트, 이벤트 검색…
        </span>
        <kbd className="ml-auto hidden shrink-0 rounded border border-[var(--border)] px-1.5 py-0.5 text-[11px] md:inline">
          Ctrl + K
        </kbd>
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent showCloseButton={false} className="overflow-hidden p-0">
          <DialogTitle className="sr-only">전역 검색</DialogTitle>
          <Command>
            <CommandInput placeholder="클러스터, 노드, GPU, 테넌트, 이벤트 검색…" />
            <CommandList>
              <CommandEmpty>결과가 없습니다.</CommandEmpty>
              <CommandGroup heading="화면 이동">
                {items.map((item) => (
                  <CommandItem
                    key={item.to}
                    value={`${item.label} ${item.section}`}
                    onSelect={() => {
                      setOpen(false);
                      onNavigate?.(item.to);
                    }}
                  >
                    <span>{item.label}</span>
                    <span className="ml-auto text-xs text-[var(--muted-foreground)]">
                      {item.section}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </DialogContent>
      </Dialog>
    </>
  );
}

/** 실시간 표시 + 현재 시각(KST). */
export function LiveClock() {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(id);
  }, []);

  return (
    <div className="flex items-center gap-3 text-sm whitespace-nowrap">
      <span className="flex items-center gap-1.5">
        <span
          className="size-2 rounded-full"
          style={{ background: 'var(--metric-pod)' }}
          aria-hidden
        />
        실시간
      </span>
      <span className="text-[var(--muted-foreground)] tabular-nums">
        {now.toLocaleTimeString('ko-KR', { hour12: false, timeZone: 'Asia/Seoul' })} (KST)
      </span>
    </div>
  );
}

/** 알림 벨 + 미확인 건수 배지. */
export function NotificationBell({ count = 0, onClick }: { count?: number; onClick?: () => void }) {
  return (
    <Button
      variant="ghost"
      size="icon"
      className="relative"
      onClick={onClick}
      aria-label={count > 0 ? `알림 ${count}건` : '알림'}
    >
      <Bell />
      {count > 0 && (
        <span
          className="absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-none font-medium text-white"
          style={{ background: 'var(--metric-fault)' }}
        >
          {count > 99 ? '99+' : count}
        </span>
      )}
    </Button>
  );
}

/** 도움말 — 단축키 안내. */
export function HelpButton() {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="도움말">
          <CircleHelp />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-64 p-3 text-sm">
        <p className="mb-2 font-medium">단축키</p>
        {/* 실제로 동작하는 키만 적는다 — 구 '/' 행(현재 화면 내 검색)은 어디에도
            핸들러가 없어, 시연 중 눌러 본 사람에게 "안내가 거짓"을 먼저 보였다. */}
        <dl className="space-y-1.5 text-[var(--muted-foreground)]">
          {[
            ['Ctrl + K', '전역 검색'],
            ['Esc', '창 닫기'],
          ].map(([key, desc]) => (
            <div key={key} className="flex items-center justify-between gap-3">
              <dt>
                <kbd className="rounded border border-[var(--border)] px-1.5 py-0.5 text-[11px]">
                  {key}
                </kbd>
              </dt>
              <dd>{desc}</dd>
            </div>
          ))}
        </dl>
      </PopoverContent>
    </Popover>
  );
}

/** 사용자 아바타 — 이니셜 2글자 + 이름(sm 이상). */
export function UserAvatar({ name = '관리자' }: { name?: string }) {
  const words = name.trim().split(/\s+/);
  const initials = (
    words.length > 1 ? words.map((w) => w[0]).join('') : name.trim().slice(0, 2)
  )
    .slice(0, 2)
    .toUpperCase();

  return (
    <div className="flex items-center gap-2">
      <span
        /* primary 위 전경색은 primary-foreground 다 — card 를 쓰면 다크모드에서
           어두운 글자가 파란 원 위에 얹혀 읽히지 않는다. */
        className="flex size-8 shrink-0 items-center justify-center rounded-full text-xs font-medium text-[var(--primary-foreground)]"
        style={{ background: 'var(--primary)' }}
        aria-hidden
      >
        {initials}
      </span>
      <span className="hidden text-sm whitespace-nowrap sm:inline">{name}</span>
    </div>
  );
}
