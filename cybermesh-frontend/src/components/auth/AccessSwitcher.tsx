import { useCallback, useState } from "react";
import { ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { useAuth } from "@/lib/auth/AuthProvider";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export default function AccessSwitcher() {
  const queryClient = useQueryClient();
  const { authEnabled, session, selectAccess } = useAuth();
  const [isUpdatingAccess, setIsUpdatingAccess] = useState(false);

  const handleAccessChange = useCallback(async (value: string) => {
    if (isUpdatingAccess) {
      return;
    }
    setIsUpdatingAccess(true);
    try {
      await selectAccess(value === "__none__" ? "" : value);
      queryClient.clear();
      toast.success("Access context updated", {
        description: value === "__none__" ? "Cleared the active access selection" : `Active access set to ${value}`,
      });
    } catch (error) {
      toast.error("Failed to update access context", {
        description: error instanceof Error ? error.message : "Unexpected error",
      });
    } finally {
      setIsUpdatingAccess(false);
    }
  }, [isUpdatingAccess, queryClient, selectAccess]);

  if (!authEnabled || !session) {
    return null;
  }

  return (
    <div className="hidden xl:flex items-center gap-2 min-w-[220px]">
      <ShieldCheck className="h-4 w-4 text-muted-foreground" />
      <Select
        value={session.active_access_id || "__none__"}
        onValueChange={handleAccessChange}
        disabled={isUpdatingAccess || !session.capabilities.can_select_access}
      >
        <SelectTrigger className="h-9 bg-background/60 border-border/60 text-xs">
          <SelectValue placeholder="Select access" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="__none__">No active access</SelectItem>
          {session.allowed_access_ids.map((accessId) => (
            <SelectItem key={accessId} value={accessId}>
              {accessId}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
