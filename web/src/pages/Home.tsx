import { SparklesIcon } from "lucide-react";
import { useState } from "react";
import AIInsightDialog from "@/components/AIInsightDialog";
import MemoView from "@/components/MemoView";
import PagedMemoList from "@/components/PagedMemoList";
import { Button } from "@/components/ui/button";
import { useInstance } from "@/contexts/InstanceContext";
import { useMemoFilters, useMemoSorting } from "@/hooks";
import useCurrentUser from "@/hooks/useCurrentUser";
import { State } from "@/types/proto/api/v1/common_pb";
import { Memo } from "@/types/proto/api/v1/memo_service_pb";

const Home = () => {
  const user = useCurrentUser();
  const { isInitialized } = useInstance();
  const [insightDialogOpen, setInsightDialogOpen] = useState(false);

  const memoFilter = useMemoFilters({
    creatorName: user?.name,
    includeShortcuts: true,
    includePinned: true,
  });

  const { listSort, orderBy } = useMemoSorting({
    pinnedFirst: true,
    state: State.NORMAL,
  });

  return (
    <div className="w-full min-h-full bg-background text-foreground">
      <PagedMemoList
        renderer={(memo: Memo) => <MemoView key={`${memo.name}-${memo.displayTime}`} memo={memo} showVisibility showPinned compact />}
        listSort={listSort}
        orderBy={orderBy}
        filter={memoFilter}
        enabled={isInitialized}
        prefixElement={
          <div className="w-full mb-2 flex justify-end">
            <Button variant="outline" size="sm" className="h-7 gap-1.5" onClick={() => setInsightDialogOpen(true)}>
              <SparklesIcon className="w-3.5 h-3.5 text-amber-500" />
              AI Insight
            </Button>
          </div>
        }
      />
      <AIInsightDialog open={insightDialogOpen} onOpenChange={setInsightDialogOpen} defaultFilter={memoFilter || ""} defaultMode="filter" />
    </div>
  );
};

export default Home;
