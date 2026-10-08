package dev.proofcode.control.experiment;

import java.util.ArrayList;
import java.util.List;

final class CsvComparison {
    private CsvComparison(){}
    static String render(ExperimentService.Comparison comparison){
        List<String> headers=new ArrayList<>(List.of("experimentId","sourceRevision","profileVersion","group","repetition","taskId","attempt","status","terminal","valid","profileApplied","configurationFailed","reason"));
        headers.addAll(ExperimentService.NUMERIC_METRICS);headers.addAll(ExperimentService.BOOLEAN_METRICS);headers.addAll(ExperimentService.MEASUREMENT_NOTES);
        StringBuilder csv=new StringBuilder(String.join(",",headers)).append("\r\n");
        for(var group:comparison.groups()) for(var result:group.results()) csv.append(row(comparison,result));
        return csv.toString();
    }
    private static String row(ExperimentService.Comparison c,ExperimentService.RunResult r){
        List<String> cells=new ArrayList<>(List.of(quote(c.experiment().getId().toString()),quote(c.experiment().getSourceRevision()),quote(c.experiment().getProfileVersion()),quote(r.group().name()),Integer.toString(r.repetition()),quote(r.taskId().toString()),r.attempt()==null?"":Integer.toString(r.attempt()),quote(r.status()),Boolean.toString(r.terminal()),Boolean.toString(r.valid()),r.profileApplied()==null?"":r.profileApplied().toString(),Boolean.toString(r.configurationFailed()),quote(r.reason())));
        for(String key:ExperimentService.NUMERIC_METRICS)cells.add(r.observedResult().containsKey(key)?r.observedResult().get(key).toString():"");
        for(String key:ExperimentService.BOOLEAN_METRICS)cells.add(r.observedResult().containsKey(key)?r.observedResult().get(key).toString():"");
        for(String key:ExperimentService.MEASUREMENT_NOTES)cells.add(r.observedResult().containsKey(key)?quote(r.observedResult().get(key).toString()):"");
        return String.join(",",cells)+"\r\n";
    }
    /** Quoting alone does not stop spreadsheet formula execution. */
    private static String quote(String value){if(value==null)return "";if(!value.isEmpty()&&"=+-@\t\r".indexOf(value.charAt(0))>=0)value="'"+value;return "\""+value.replace("\"","\"\"")+"\"";}
}
