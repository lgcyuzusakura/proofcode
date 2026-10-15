package dev.proofcode.control.experiment;

/** Immutable ablation profiles. The mandatory execution boundary is never ablated. */
public record ExperimentProfile(String version,Group group,boolean toolsEnabled,boolean mandatorySafety,
    boolean deterministicSafety,boolean jevRouting,boolean jevRequired,boolean ragEnabled,
    boolean contextCompressionEnabled,boolean feedbackRetrieval) {
    public static final String VERSION="proofcode.experiment.v1";
    public enum Group { A,B,C,D,E,F }
    public static ExperimentProfile forGroup(Group group){
        return switch(group){
            case A -> new ExperimentProfile(VERSION,group,false,true,false,false,false,false,false,false);
            case B -> new ExperimentProfile(VERSION,group,true,true,true,false,false,false,false,false);
            case C -> new ExperimentProfile(VERSION,group,true,true,false,true,true,false,false,false);
            case D -> new ExperimentProfile(VERSION,group,true,true,false,false,false,true,false,false);
            case E -> new ExperimentProfile(VERSION,group,true,true,false,false,false,true,true,false);
            case F -> new ExperimentProfile(VERSION,group,true,true,true,true,true,true,true,true);
        };
    }
}
