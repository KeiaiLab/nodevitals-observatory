// ContactBubble — 우하단 고정 "문의하기" 버블(데모 모드 전용).
//
// 공개 데모를 본 방문자가 문의를 남기면 서버가 받아 보관한다. 접수 경로는
// 무인증이라 서버가 필드 검증·접수 상한을 갖고 있고, 여기서는 그 거절을
// 방문자가 이해할 수 있는 문장으로 되돌려준다(400 = 입력 문제 / 429 = 잠시 후).
//
// 이 자리에 있던 시연 리모컨은 제거됐다 — 시나리오는 무인 자동 루프로
// 진행되고, 승인·번인 등 조작은 각 페이지의 액션 버튼이 담당한다.
import { Check, MessageCircle, X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { submitContact } from '@/lib/demoApi';

const EMPTY_FORM = { name: '', org: '', email: '', message: '' };

export default function ContactBubble() {
  const [open, setOpen] = useState(false);
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState(EMPTY_FORM);
  const firstFieldRef = useRef<HTMLInputElement>(null);

  // 펼칠 때 첫 입력으로 포커스를 옮긴다 — 키보드 사용자가 버블을 연 뒤 다시
  // Tab 으로 폼을 찾아 들어가지 않도록.
  useEffect(() => {
    if (open && !sent) firstFieldRef.current?.focus();
  }, [open, sent]);

  // Esc 로 닫기 — 화면 위에 떠 있는 요소의 기본 기대.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open]);

  const set = (k: keyof typeof form) => (e: { target: { value: string } }) =>
    setForm((f) => ({ ...f, [k]: e.target.value }));

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (sending) return;
    setSending(true);
    setError(null);
    const outcome = await submitContact(form);
    setSending(false);
    if (!outcome.ok) {
      setError(outcome.error);
      return;
    }
    setSent(true);
    setForm(EMPTY_FORM);
  }

  function close() {
    setOpen(false);
    // 닫으면 접수 완료 화면을 되돌린다 — 다음에 열었을 때 빈 폼으로 시작해야
    // 두 번째 문의를 남길 수 있다.
    setSent(false);
    setError(null);
  }

  if (!open) {
    return (
      <Button
        className="fixed right-4 bottom-4 z-50 h-11 rounded-full shadow-lg"
        onClick={() => setOpen(true)}
        aria-label="문의하기 열기"
      >
        <MessageCircle className="size-4" />
        문의하기
      </Button>
    );
  }

  return (
    <Card
      className="fixed right-4 bottom-4 z-50 w-80 border shadow-lg backdrop-blur supports-[backdrop-filter]:bg-card/95"
      role="dialog"
      aria-label="문의하기"
    >
      <CardContent className="flex flex-col gap-3 p-4">
        <div className="flex items-center justify-between gap-2">
          <span className="text-sm font-medium">문의하기</span>
          <Button variant="ghost" size="sm" className="h-7 px-2" onClick={close} aria-label="닫기">
            <X className="size-3.5" />
          </Button>
        </div>

        {sent ? (
          <div className="flex flex-col gap-3">
            <p className="flex items-start gap-2 text-xs leading-relaxed text-muted-foreground">
              <Check className="mt-0.5 size-4 shrink-0 text-metric-pod" />
              문의가 접수됐습니다. 남겨주신 연락처로 회신드리겠습니다.
            </p>
            <Button variant="outline" size="sm" className="h-8" onClick={() => setSent(false)}>
              추가로 문의하기
            </Button>
          </div>
        ) : (
          <form className="flex flex-col gap-2.5" onSubmit={onSubmit}>
            <div className="flex flex-col gap-1">
              <Label htmlFor="contact-name" className="text-xs">
                이름
              </Label>
              <Input
                id="contact-name"
                ref={firstFieldRef}
                value={form.name}
                onChange={set('name')}
                className="h-8 text-sm"
                required
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="contact-org" className="text-xs">
                소속 <span className="text-muted-foreground">(선택)</span>
              </Label>
              <Input
                id="contact-org"
                value={form.org}
                onChange={set('org')}
                className="h-8 text-sm"
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="contact-email" className="text-xs">
                이메일 <span className="text-muted-foreground">(선택)</span>
              </Label>
              <Input
                id="contact-email"
                type="email"
                value={form.email}
                onChange={set('email')}
                className="h-8 text-sm"
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="contact-message" className="text-xs">
                문의 내용
              </Label>
              <Textarea
                id="contact-message"
                value={form.message}
                onChange={set('message')}
                rows={4}
                className="resize-none text-sm"
                required
              />
            </div>
            {error ? <p className="text-[11px] leading-snug text-destructive">{error}</p> : null}
            <Button type="submit" size="sm" className="h-8" disabled={sending}>
              {sending ? '보내는 중…' : '보내기'}
            </Button>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
