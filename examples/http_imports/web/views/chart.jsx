import React from "react";
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip } from "https://esm.sh/recharts@2.12.0";
import { format } from "https://esm.sh/date-fns@3.6.0";

function ChartView() {
    const serverData = useBungoData();
    const rawPoints = Array.isArray(serverData.Points) ? serverData.Points : [];

    const points = React.useMemo(() => {
        return rawPoints.map((point) => {
            const label = point?.date ? format(new Date(point.date), "MMM d") : "n/a";
            return {
                day: label,
                users: Number(point?.users || 0),
            };
        });
    }, [rawPoints]);

    return (
        <div>
            <LineChart width={900} height={360} data={points}>
                <CartesianGrid strokeDasharray="3 3" stroke="#30363d" />
                <XAxis dataKey="day" stroke="#8b949e" />
                <YAxis stroke="#8b949e" />
                <Tooltip />
                <Line type="monotone" dataKey="users" stroke="#58a6ff" strokeWidth={3} />
            </LineChart>
        </div>
    );
}

_bungoRender(ChartView);
