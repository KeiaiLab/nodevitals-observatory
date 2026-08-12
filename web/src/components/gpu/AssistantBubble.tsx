// AssistantBubble — 우하단 고정 "GPU Assistant" 진입 버블(데모 모드 전용).
// 기존 문의하기 버블을 대체한다(사용자 지시 2026-07-28): 어느 GPU 화면에서든
// 진행 중 장애를 문맥으로 어시스턴트에게 물어보러 들어가는 단일 진입점이다.
// 어시스턴트 화면(/gpu/assistant) 자신에서는 숨긴다(자기 진입 버블 불필요).
import { Bot } from 'lucide-react';
import { useLocation, useNavigate } from 'react-router';
import { Button } from '@/components/ui/button';

export default function AssistantBubble() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  if (pathname === '/gpu/assistant') return null;

  return (
    <Button
      className="fixed right-4 bottom-4 z-50 h-11 rounded-full shadow-lg"
      onClick={() => navigate('/gpu/assistant')}
      aria-label="GPU Assistant 열기"
    >
      <Bot className="size-4" />
      GPU Assistant
    </Button>
  );
}
